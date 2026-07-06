package services

import (
	"context"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
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
	users      CalendarFeedUserReader
	days       CalendarFeedDayReader
	disclaimer DisclaimerProvider
}

// CalendarFeedUserReader resolves the single owner whose calendar_feed_selector
// equals selector. It returns (user, false, nil) for no match — the same shape
// FindByCalendarFeedSelector uses — so the service keeps an unknown selector and
// a wrong verifier observationally identical.
type CalendarFeedUserReader interface {
	FindByCalendarFeedSelector(ctx context.Context, selector string) (models.User, bool, error)
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

// NewCalendarFeedService wires the feed service from the user + day readers and
// the localized-disclaimer provider (the same seam the webhook notify pass
// uses). All three are required in production; tests may pass stubs.
func NewCalendarFeedService(users CalendarFeedUserReader, days CalendarFeedDayReader, disclaimer DisclaimerProvider) *CalendarFeedService {
	return &CalendarFeedService{
		users:      users,
		days:       days,
		disclaimer: disclaimer,
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
// Timing-equalization (mirrors equalizeAuthCredentialsTiming): the happy path
// pays exactly one bcrypt compare inside VerifyCalendarFeedToken. The
// selector-miss path would otherwise short-circuit before any bcrypt work and
// leak — through response latency — that no row bears the presented selector
// (CWE-208). So on a selector miss the service performs one dummy bcrypt against
// a fixed placeholder hash, matching the happy path's single compare and leaving
// no timing signal that separates "unknown selector" from "known selector, wrong
// verifier". A malformed token (wrong length) is rejected before any lookup and
// is not timing-sensitive: it reveals nothing about which selectors exist.
func (service *CalendarFeedService) ResolveFeed(ctx context.Context, token string, now time.Time, location *time.Location) (body []byte, ok bool, err error) {
	selector, _, split := SplitCalendarFeedToken(token)
	if !split {
		return nil, false, nil
	}

	user, found, err := service.users.FindByCalendarFeedSelector(ctx, selector)
	if err != nil {
		return nil, false, err
	}
	if !found {
		// Selector resolves no row: spend the same single bcrypt the happy path
		// spends so an unknown selector is timing-indistinguishable from a known
		// one with a bad verifier.
		equalizeCalendarFeedTiming(token)
		return nil, false, nil
	}

	if !VerifyCalendarFeedToken(token, user.CalendarFeedSelector, user.CalendarFeedVerifierHash) {
		return nil, false, nil
	}

	// Verified. Every read below is scoped strictly to the resolved user.ID —
	// the request carried no user id, only the token, so the owner boundary is
	// exactly the row the token resolved to.
	feedLocation := location
	if feedLocation == nil {
		feedLocation = time.UTC
	}
	today := DateAtLocation(now, feedLocation)
	from := today.AddDate(-calendarFeedStatsWindowYears, 0, 0)
	logs, err := service.days.FetchLogsForUser(ctx, user.ID, from, today, feedLocation)
	if err != nil {
		return nil, false, err
	}

	disclaimer := ""
	if service.disclaimer != nil {
		// No per-owner language is persisted (mirrors the webhook notify pass):
		// resolve the disclaimer at the server-default language. Messages merges
		// the default over an empty/unknown target, so "" yields the default copy.
		disclaimer = service.disclaimer.Disclaimer("")
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

// equalizeCalendarFeedTiming performs the single dummy bcrypt compare that keeps
// the selector-miss path's latency indistinguishable from a real verification.
// It is declared as a var so tests can replace it with an invocation counter and
// assert "a dummy bcrypt occurred on the selector-miss path" without measuring
// wall-clock time — the same test-substitution idiom as
// equalizeAuthCredentialsTiming. Production never reassigns it. It reuses the
// fixed placeholder hash whose embedded cost is pinned to passwordHashCost, so
// the equalized path is never cheaper than the real bcrypt in
// VerifyCalendarFeedToken.
var equalizeCalendarFeedTiming = func(token string) {
	_ = bcrypt.CompareHashAndPassword([]byte(credentialsTimingEqualizationHash), []byte(token))
}
