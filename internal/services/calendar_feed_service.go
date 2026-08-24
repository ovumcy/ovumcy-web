package services

import (
	"context"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// CalendarFeedService resolves a calendar-feed bearer token to its owner and
// renders that owner's read-only .ics body (issue #126-replacement / .ics,
// slice 3). It is the business-logic seam the api layer calls: the api layer
// stays transport-only (it never touches a repository, bcrypt, or the token
// split), and every security decision — constant-time verification,
// timing-equalization against selector enumeration, owner scoping, and
// prediction suppression — lives here.
//
// The token IS the authorization: there is no session, cookie, or user id in
// the request. ResolveFeed splits the token, looks the row up by its non-secret
// selector, constant-time-verifies the secret verifier, and — only on success —
// loads that resolved owner's logs (scoped strictly to the resolved user id) and
// builds the feed. A malformed token, an unknown selector, a wrong verifier, and
// a disabled feed are ALL reported the same way (ok=false, no body), so a caller
// (and thus the HTTP surface) gets no oracle distinguishing them.
type CalendarFeedService struct {
	users      CalendarFeedUserStore
	days       CalendarFeedDayReader
	disclaimer DisclaimerProvider
	secretKey  []byte
}

// CalendarFeedUserStore resolves the single owner whose calendar_feed_selector
// equals selector, and writes the keyed verifier MAC into a row that predates
// migration 032. The lookup returns (user, false, nil) for no match — the same
// shape the repository's FindByCalendarFeedSelector uses — so the service keeps
// an unknown selector and a wrong verifier observationally identical.
//
// It is a store rather than a reader because the one write it carries is not a
// domain decision: the MAC can only be computed while the correct verifier
// plaintext is in hand, which happens exactly once per legacy row, on the read
// path. See BackfillCalendarFeedVerifierMAC for the compare-and-set that keeps
// that write safe against a concurrent rotation.
type CalendarFeedUserStore interface {
	FindByCalendarFeedSelector(ctx context.Context, selector string) (models.User, bool, error)
	BackfillCalendarFeedVerifierMAC(ctx context.Context, userID uint, selector string, verifierMAC string) error
}

// CalendarFeedDayReader loads the resolved owner's day logs for the prediction
// window. It is the same read the dashboard/stats path uses; the feed passes a
// bounded recent range so the .ics reflects the owner's current cycle history.
type CalendarFeedDayReader interface {
	FetchLogsForUser(ctx context.Context, userID uint, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error)
}

// calendarFeedStatsWindowYears bounds the log history loaded to compute the
// feed's predictions. It mirrors the dashboard/stats 2-year window so the feed's
// cycle baseline is derived from the same span the in-app surfaces use.
const calendarFeedStatsWindowYears = 2

// NewCalendarFeedService wires the feed service from the user store + day reader,
// the localized-disclaimer provider (the same seam the webhook notify pass uses),
// and the application secret that keys verifier MACs. All are required in
// production; tests may pass stubs.
func NewCalendarFeedService(users CalendarFeedUserStore, days CalendarFeedDayReader, disclaimer DisclaimerProvider, secretKey []byte) *CalendarFeedService {
	return &CalendarFeedService{
		users:      users,
		days:       days,
		disclaimer: disclaimer,
		secretKey:  secretKey,
	}
}

// ResolveFeed authenticates a feed token and returns the owner's rendered .ics
// body. ok is false — with no body — for EVERY failure mode (malformed token,
// unknown selector, wrong verifier, disabled feed); the caller maps that single
// outcome to a bare 404, giving no oracle. err is non-nil only for an
// infrastructure failure (a DB or log-read error), which the caller maps to a
// generic 500 — never a body that distinguishes it from the 404 path in a way
// that leaks token state.
//
// location is the FALLBACK zone only: the resolved owner's persisted timezone
// decides which calendar day the feed renders, and the caller's location is used
// only when that column is empty or unusable (see the resolution comment below).
//
// Timing-equalization (mirrors equalizeAuthCredentialsTiming): the verify path
// pays exactly one keyed-MAC compare inside VerifyCalendarFeedToken. The
// selector-miss path would otherwise short-circuit before any verifier work and
// leak — through response latency — that no row bears the presented selector
// (CWE-208). So on a selector miss the service performs one dummy MAC compare
// against a fixed placeholder, matching the verify path's single compare and
// leaving no timing signal that separates "unknown selector" from "known
// selector, wrong verifier". A malformed token (wrong length) is rejected before
// any lookup and is not timing-sensitive: it reveals nothing about which
// selectors exist.
//
// The equalization must track whatever primitive the verify path actually runs,
// or it stops equalizing anything. That is why it moved off bcrypt in lockstep
// with the verifier: a MAC-based verify path paired with a bcrypt-based dummy
// would have kept the exact CPU cost this design removes — every random token
// would still burn ~265 ms on the miss path — while a bcrypt verify path paired
// with a MAC-based dummy would hand back the enumeration oracle.
//
// ONE transitional exception, bounded and self-healing: a row minted before
// migration 032 has no MAC, so verifying it costs one bcrypt. Until its first
// successful poll writes the MAC in, that row's wrong-verifier latency is
// distinguishable from a selector miss. Reaching that signal requires already
// holding a valid 80-bit selector (a leaked subscribe URL carries both halves, so
// there is no realistic path to the selector alone), and the feed's own per-IP
// budget caps the attempt rate. Documented in SECURITY.md and
// docs/SECURITY_INVARIANTS.md rather than papered over.
func (service *CalendarFeedService) ResolveFeed(ctx context.Context, token string, now time.Time, location *time.Location) (body []byte, ok bool, err error) {
	selector, verifier, split := SplitCalendarFeedToken(token)
	if !split {
		return nil, false, nil
	}

	user, found, err := service.users.FindByCalendarFeedSelector(ctx, selector)
	if err != nil {
		return nil, false, err
	}
	if !found {
		// Selector resolves no row: spend the same single MAC compare the verify
		// path spends so an unknown selector is timing-indistinguishable from a
		// known one with a bad verifier.
		equalizeCalendarFeedTiming(service.secretKey, selector, verifier)
		return nil, false, nil
	}

	stored := models.CalendarFeedTokenColumns{
		Selector:     user.CalendarFeedSelector,
		VerifierHash: user.CalendarFeedVerifierHash,
		VerifierMAC:  user.CalendarFeedVerifierMAC,
	}
	if !VerifyCalendarFeedToken(service.secretKey, token, stored) {
		return nil, false, nil
	}
	if stored.VerifierMAC == "" {
		// The row verified through the pre-032 bcrypt path, which means this is the
		// only moment its MAC can be derived: the correct verifier plaintext is in
		// hand and is never persisted. Write it in so the next poll takes the fast
		// path. A failure here is a missed optimization, not a reason to refuse a
		// token that just verified — the feed is served either way and the next
		// poll retries.
		service.backfillVerifierMAC(ctx, user.ID, selector, verifier)
	}

	// Verified. Every read below is scoped strictly to the resolved user.ID —
	// the request carried no user id, only the token, so the owner boundary is
	// exactly the row the token resolved to.
	//
	// "Today" resolves in the OWNER's persisted timezone, not the transport's.
	// The location the api layer passes in is the ordinary request chain
	// (X-Ovumcy-Timezone header -> ovumcy_tz cookie -> server zone), and a
	// calendar client sends neither the header nor the cookie — so that chain
	// always collapses to the server zone and would render an owner in a distant
	// timezone a day off. users.timezone exists for exactly these request-free
	// passes, and the sibling egress subsystem (the webhook notify pass) already
	// resolves the owner's day through the same helper; sharing it keeps the two
	// from disagreeing about which calendar day the owner is on. The passed-in
	// location stays the fallback for an owner whose timezone was never captured.
	requestLocation := location
	if requestLocation == nil {
		requestLocation = time.UTC
	}
	feedLocation := resolveOwnerLocation(user.Timezone, requestLocation)
	today := DateAtLocation(now, feedLocation)
	from := today.AddDate(-calendarFeedStatsWindowYears, 0, 0)
	logs, err := service.days.FetchLogsForUser(ctx, user.ID, from, today, feedLocation)
	if err != nil {
		return nil, false, err
	}

	disclaimer := ""
	if service.disclaimer != nil {
		// Resolve at the OWNER's chosen language (users.interface_language, the
		// durable carrier of that choice), not at the server default: a calendar
		// client sends no Accept-Language and no cookie, so the stored column is the
		// only thing that knows which language this owner reads — exactly as
		// users.timezone is the only thing that knows their calendar day. The
		// webhook notify pass resolves the same field the same way. An owner who
		// never chose a language stores "", and Messages merges the default over an
		// empty or unknown target, so "" still yields the server-default copy.
		disclaimer = service.disclaimer.Disclaimer(user.InterfaceLanguage)
	}

	feedUser := user
	feed := BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       &feedUser,
		Logs:       logs,
		Now:        now,
		Location:   feedLocation,
		Disclaimer: disclaimer,
	})
	return feed, true, nil
}

// backfillVerifierMAC derives the keyed verifier MAC for a row minted before
// migration 032 and hands it to the repository's compare-and-set write. It is
// reached only after VerifyCalendarFeedToken has already accepted the presented
// token through the bcrypt path, so the verifier is known-correct here.
//
// Every failure is swallowed on purpose, and each one is benign: a derivation
// error means no secret key (the row simply stays on the bcrypt path), and a
// write error or a zero-row CAS outcome means the feed was rotated or revoked
// underneath this request — in which case NOT writing is the correct result. The
// caller has already decided the token is valid, and none of these outcomes may
// turn a valid feed into a 404 or a 500.
func (service *CalendarFeedService) backfillVerifierMAC(ctx context.Context, userID uint, selector string, verifier string) {
	verifierMAC, err := security.CalendarFeedVerifierMAC(service.secretKey, selector, verifier)
	if err != nil {
		return
	}
	_ = service.users.BackfillCalendarFeedVerifierMAC(ctx, userID, selector, verifierMAC)
}

// calendarFeedMACTimingEqualizationValue is a fixed placeholder authenticator of
// the same hex width a real verifier MAC has. It must stay non-empty: an empty MAC
// is the marker for a pre-032 row, and the stand-in row below carries no bcrypt
// hash, so an empty placeholder would make verification refuse before doing any
// work — silently turning the equalized path free.
const calendarFeedMACTimingEqualizationValue = "9f2b7c41d8e6053a1cbf47d92e80a5361f4c8b0d75e29a3612bd0c8f47e5a9d3"

// equalizeCalendarFeedTiming performs the single dummy verifier compare that
// keeps the selector-miss path's latency indistinguishable from a real
// verification. It is declared as a var so tests can replace it with an
// invocation counter and assert "a dummy verifier compare occurred on the
// selector-miss path" without measuring wall-clock time — the same
// test-substitution idiom as equalizeAuthCredentialsTiming. Production never
// reassigns it.
//
// It calls VerifyCalendarFeedToken itself, against a stand-in row that carries the
// placeholder MAC and no bcrypt hash. That is deliberate: the equalization cannot
// approximate the verify path's cost, it re-executes it, so whatever primitive
// verification uses the dummy uses too and the two can never drift apart. An
// equalization that hardcoded its own primitive would silently stop equalizing the
// day the verifier changed — which is exactly what happened when the verifier moved
// from bcrypt to a keyed MAC.
//
// The comparison can never succeed — and if it somehow did, nothing observes the
// result: the caller has already resolved no row and returns not-found.
var equalizeCalendarFeedTiming = func(secretKey []byte, selector string, verifier string) {
	_ = VerifyCalendarFeedToken(secretKey, selector+verifier, models.CalendarFeedTokenColumns{
		Selector:    selector,
		VerifierMAC: calendarFeedMACTimingEqualizationValue,
	})
}
