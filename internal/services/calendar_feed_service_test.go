package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// calendarFeedTestSecretKey keys the verifier MACs minted by these tests. A
// second, different key stands in for a rotated SECRET_KEY.
const (
	calendarFeedTestSecretKey  = "calendar-feed-test-key"
	calendarFeedRotatedTestKey = "calendar-feed-rotated-test-key"
)

// stubFeedUserStore resolves a single armed owner by selector. Any other selector
// (or the empty string) is a miss, mirroring FindByCalendarFeedSelector. It also
// records the MAC backfill the feed performs for a pre-032 row, and applies it to
// the stored row the way the real compare-and-set does, so a test can poll twice
// and observe the row leaving the bcrypt path.
type stubFeedUserStore struct {
	selector string
	user     models.User
	err      error
	calls    int

	backfillCalls    int
	backfilledUserID uint
	backfilledSel    string
	backfilledMAC    string
	backfillErr      error
}

func (s *stubFeedUserStore) FindByCalendarFeedSelector(_ context.Context, selector string) (models.User, bool, error) {
	s.calls++
	if s.err != nil {
		return models.User{}, false, s.err
	}
	if selector != "" && selector == s.selector {
		return s.user, true, nil
	}
	return models.User{}, false, nil
}

func (s *stubFeedUserStore) BackfillCalendarFeedVerifierMAC(_ context.Context, userID uint, selector string, verifierMAC string) error {
	s.backfillCalls++
	s.backfilledUserID = userID
	s.backfilledSel = selector
	s.backfilledMAC = verifierMAC
	if s.backfillErr != nil {
		return s.backfillErr
	}
	s.user.CalendarFeedVerifierMAC = verifierMAC
	return nil
}

// stubFeedDayReader records the userID it was asked for so a test can prove the
// feed scopes its log read to the resolved owner, plus the range upper bound
// (the owner's "today") and the zone it was resolved in, so a test can prove
// which timezone won — the owner's stored one or the transport's fallback.
//
// requestedLocation is compared by POINTER identity in the fallback cases: the
// "Local" token resolves to time.Local, which stringifies to the host's real zone
// name whenever TZ is set, so a name comparison would pass on some hosts and fail
// on others. Identity with the injected fallback is host-independent.
type stubFeedDayReader struct {
	logs              []models.DailyLog
	err               error
	requestedUser     uint
	requestedTo       time.Time
	requestedLocation *time.Location
	requestedCount    int
}

func (s *stubFeedDayReader) FetchLogsForUser(_ context.Context, userID uint, _ time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error) {
	s.requestedUser = userID
	s.requestedTo = to
	s.requestedLocation = location
	s.requestedCount++
	if s.err != nil {
		return nil, s.err
	}
	return s.logs, nil
}

type stubFeedDisclaimer struct{ text string }

func (s stubFeedDisclaimer) Disclaimer(string) string { return s.text }

// armedFeedUser mints a real token under calendarFeedTestSecretKey and returns the
// owner row that would resolve it, plus the full token to present. A real
// GenerateCalendarFeedToken keeps both stored verifier columns — keyed MAC and
// bcrypt hash — exercised end-to-end.
func armedFeedUser(t *testing.T, id uint, lastPeriodStart string) (models.User, string) {
	t.Helper()
	token, columns, err := GenerateCalendarFeedToken([]byte(calendarFeedTestSecretKey))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	start := mustParseDashboardDay(t, lastPeriodStart)
	user := models.User{
		ID:                       id,
		CycleLength:              28,
		PeriodLength:             5,
		LutealPhase:              14,
		LastPeriodStart:          &start,
		CalendarFeedSelector:     columns.Selector,
		CalendarFeedVerifierHash: columns.VerifierHash,
		CalendarFeedVerifierMAC:  columns.VerifierMAC,
	}
	return user, token
}

// legacyArmedFeedUser returns the same row as it would have been stored BEFORE
// migration 032: a bcrypt hash and no MAC. Such a row can only be verified
// through the bcrypt path, and its MAC cannot be derived from storage — only from
// a request that presents the correct verifier.
func legacyArmedFeedUser(t *testing.T, id uint, lastPeriodStart string) (models.User, string) {
	t.Helper()
	user, token := armedFeedUser(t, id, lastPeriodStart)
	user.CalendarFeedVerifierMAC = ""
	return user, token
}

// mustSplitFeedToken splits a token that was just minted for this test,
// failing the test (not the assertion actually under test) if it doesn't
// split at the expected width — that would be a test-setup bug, not the
// no-oracle behavior these regressions exist to prove.
func mustSplitFeedToken(t *testing.T, token string) (selector string, verifier string) {
	t.Helper()
	selector, verifier, ok := SplitCalendarFeedToken(token)
	if !ok {
		t.Fatalf("SplitCalendarFeedToken: a freshly armed token must split, got token of length %d", len(token))
	}
	return selector, verifier
}

func newFeedServiceForTest(user models.User, logs []models.DailyLog) (*CalendarFeedService, *stubFeedUserStore, *stubFeedDayReader) {
	users := &stubFeedUserStore{selector: user.CalendarFeedSelector, user: user}
	days := &stubFeedDayReader{logs: logs}
	svc := NewCalendarFeedService(users, days, stubFeedDisclaimer{text: "estimates disclaimer"}, []byte(calendarFeedTestSecretKey))
	return svc, users, days
}

func TestResolveFeedReturnsOwnersFeedForValidToken(t *testing.T) {
	user, token := armedFeedUser(t, 42, "2026-03-02")
	logs := predictableFeedLogs(t)
	svc, _, days := newFeedServiceForTest(user, logs)

	body, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok for a valid token")
	}
	if days.requestedUser != 42 {
		t.Fatalf("expected log read scoped to resolved owner 42, got %d", days.requestedUser)
	}
	text := string(body)
	if !strings.Contains(text, "BEGIN:VCALENDAR") || !strings.Contains(text, "BEGIN:VEVENT") {
		t.Fatalf("expected a populated .ics for a predictable owner, got:\n%s", text)
	}
	// The medical-safety disclaimer must ride along on the .ics feed — a
	// predictive calendar surface. Pinning the exact stub text is the sanctioned
	// medical-safety copy assertion, and it kills the calendar_feed_service.go:117
	// guard mutant that would drop the disclaimer (Disclaimer: "") from the feed.
	if !strings.Contains(text, "estimates disclaimer") {
		t.Fatalf("expected the medical-safety disclaimer in the .ics feed body, got:\n%s", text)
	}
}

// TestResolveFeedFallsBackToRequestTimezoneWithoutStoredOwnerTimezone pins the
// FALLBACK half of the feed's timezone contract: an owner whose timezone was
// never captured has no stored zone to prefer, so "today" (the log-read window's
// upper bound) falls back to the location the transport passed in. now is 23:00
// UTC, already the next calendar day in a UTC+3 zone, so today must be that next
// day rather than the UTC one.
//
// It formerly asserted this as the ONLY rule — the feed resolved "today" purely
// from the request chain. That chain (X-Ovumcy-Timezone header -> ovumcy_tz
// cookie -> server zone) is empty for a calendar client, so it always collapsed
// to the server zone; the stored owner timezone now decides first
// (TestResolveFeedPrefersOwnerStoredTimezoneOverRequestChain). The assertion is
// unchanged because this owner stores no timezone — the precondition is now
// explicit below.
//
// Also kills the nil-location guard mutant in ResolveFeed, which would substitute
// UTC and read the wrong day's window.
func TestResolveFeedFallsBackToRequestTimezoneWithoutStoredOwnerTimezone(t *testing.T) {
	user, token := armedFeedUser(t, 51, "2026-03-02")
	if user.Timezone != "" {
		t.Fatalf("test setup: this case needs an owner with no stored timezone, got %q", user.Timezone)
	}
	svc, _, days := newFeedServiceForTest(user, predictableFeedLogs(t))

	// 2026-03-10 23:00 UTC == 2026-03-11 02:00 in UTC+3.
	loc := time.FixedZone("test+3", 3*60*60)
	now := time.Date(2026, time.March, 10, 23, 0, 0, 0, time.UTC)

	if _, ok, err := svc.ResolveFeed(context.Background(), token, now, loc); err != nil || !ok {
		t.Fatalf("expected the feed to resolve: ok=%v err=%v", ok, err)
	}
	if y, m, d := days.requestedTo.Date(); y != 2026 || m != time.March || d != 11 {
		t.Fatalf("today must fall back to the request timezone (want 2026-03-11), got %04d-%02d-%02d", y, m, d)
	}
	if days.requestedLocation != loc {
		t.Fatalf("the injected request location must be used verbatim, got %v", days.requestedLocation)
	}
}

// TestResolveFeedPrefersOwnerStoredTimezoneOverRequestChain is the UTC-boundary
// regression for the day-shifted feed. A calendar client sends neither
// X-Ovumcy-Timezone nor the ovumcy_tz cookie, so the request chain the api layer
// resolves collapses to the SERVER zone; an owner in Pacific/Kiritimati (UTC+14)
// on a UTC server therefore saw a feed built around the server's calendar day.
// The sibling egress pass (webhook reminders) already resolved that owner's day
// from users.timezone, so the same prediction rendered a day apart depending on
// which channel delivered it.
//
// At 2026-03-10 23:00 UTC the owner is already on 2026-03-11, and the fixture
// puts a projected ovulation event on 2026-03-10 — a day that is "today" (kept)
// in the request zone and "yesterday" (dropped) in the owner's. So the two
// renders differ in their VEVENT set, which is the positive anchor: this test
// cannot pass while the preference is dead.
func TestResolveFeedPrefersOwnerStoredTimezoneOverRequestChain(t *testing.T) {
	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatalf("load Pacific/Kiritimati: %v", err)
	}

	// 2026-03-10 23:00 UTC == 2026-03-11 13:00 in Pacific/Kiritimati (UTC+14).
	now := time.Date(2026, time.March, 10, 23, 0, 0, 0, time.UTC)
	logs := dayBoundaryFeedLogs(t)

	owner, token := armedFeedUser(t, 61, "2026-02-25")
	owner.Timezone = "Pacific/Kiritimati"
	svc, _, days := newFeedServiceForTest(owner, logs)

	ownerBody, ok, err := svc.ResolveFeed(context.Background(), token, now, time.UTC)
	if err != nil || !ok {
		t.Fatalf("expected the feed to resolve: ok=%v err=%v", ok, err)
	}
	if y, m, d := days.requestedTo.Date(); y != 2026 || m != time.March || d != 11 {
		t.Fatalf("today must be resolved in the owner's stored timezone (want 2026-03-11), got %04d-%02d-%02d", y, m, d)
	}
	if days.requestedLocation.String() != kiritimati.String() {
		t.Fatalf("the owner-scoped log read must run in the owner's zone, got %v", days.requestedLocation)
	}

	// Same owner, same instant, same logs — but no stored timezone, so the render
	// falls back to the server/request zone (UTC), where 2026-03-10 is still today.
	fallbackOwner := owner
	fallbackOwner.Timezone = ""
	fallbackSvc, _, fallbackDays := newFeedServiceForTest(fallbackOwner, logs)
	requestBody, ok, err := fallbackSvc.ResolveFeed(context.Background(), token, now, time.UTC)
	if err != nil || !ok {
		t.Fatalf("expected the request-zone feed to resolve: ok=%v err=%v", ok, err)
	}
	if y, m, d := fallbackDays.requestedTo.Date(); y != 2026 || m != time.March || d != 10 {
		t.Fatalf("test setup: the request-zone render must sit on 2026-03-10, got %04d-%02d-%02d", y, m, d)
	}

	// Positive anchor: the two zones produce literally different calendar bodies.
	if string(ownerBody) == string(requestBody) {
		t.Fatal("test setup: the two zones must render different feeds, otherwise this test cannot observe the preference")
	}
	const boundaryEvent = "DTSTART;VALUE=DATE:20260310"
	if !strings.Contains(string(requestBody), boundaryEvent) {
		t.Fatalf("test setup: the request-zone render must keep the %s event, got:\n%s", boundaryEvent, requestBody)
	}
	if strings.Contains(string(ownerBody), boundaryEvent) {
		t.Fatalf("the owner-zone render must drop the %s event (already yesterday at UTC+14), got:\n%s", boundaryEvent, ownerBody)
	}
	if !strings.Contains(string(ownerBody), "BEGIN:VEVENT") {
		t.Fatalf("the owner-zone render must still emit its remaining events, got:\n%s", ownerBody)
	}
}

// TestResolveFeedIgnoresUnusableStoredOwnerTimezone covers the other fallback
// arm: a stored value that is not a usable IANA zone must be ignored in favour of
// the location the transport passed in, without panicking.
//
// "Local" is called out explicitly because it does NOT fail to load —
// time.LoadLocation("Local") returns the server's own zone — so a name-based
// check would silently pin the owner to the server timezone, the exact bug this
// change removes. Only validated names are ever written to the column; this is
// the defense-in-depth pin for a hand-edited or restored row.
func TestResolveFeedIgnoresUnusableStoredOwnerTimezone(t *testing.T) {
	// 2026-03-10 23:00 UTC == 2026-03-11 02:00 in the injected UTC+3 fallback.
	requestZone := time.FixedZone("test+3", 3*60*60)
	now := time.Date(2026, time.March, 10, 23, 0, 0, 0, time.UTC)

	for name, stored := range map[string]string{
		"garbage":    "Not/AZone",
		"localToken": "Local",
		"lowercased": "local",
		"whitespace": "   ",
	} {
		t.Run(name, func(t *testing.T) {
			owner, token := armedFeedUser(t, 62, "2026-03-02")
			owner.Timezone = stored
			svc, _, days := newFeedServiceForTest(owner, predictableFeedLogs(t))

			body, ok, err := svc.ResolveFeed(context.Background(), token, now, requestZone)
			if err != nil || !ok {
				t.Fatalf("an unusable stored timezone must not break the feed: ok=%v err=%v", ok, err)
			}
			if !strings.Contains(string(body), "BEGIN:VCALENDAR") {
				t.Fatalf("expected a well-formed feed, got:\n%s", body)
			}
			if days.requestedLocation != requestZone {
				t.Fatalf("expected the injected request location to be used verbatim, got %v", days.requestedLocation)
			}
			if y, m, d := days.requestedTo.Date(); y != 2026 || m != time.March || d != 11 {
				t.Fatalf("today must come from the request zone (want 2026-03-11), got %04d-%02d-%02d", y, m, d)
			}
		})
	}
}

// TestResolveFeedCrossUserIsolation is the headline test: user A's token must
// NEVER return user B's events. Only A's selector is armed in the reader; A's
// token resolves A, and the log read is scoped to A's id — B's id is never
// requested.
func TestResolveFeedCrossUserIsolation(t *testing.T) {
	userA, tokenA := armedFeedUser(t, 100, "2026-03-02")
	_, tokenB := armedFeedUser(t, 200, "2026-03-05")

	// Reader knows only user A. B's marker logs would be a privacy breach if ever
	// surfaced through A's feed.
	days := &stubFeedDayReader{logs: predictableFeedLogs(t)}
	users := &stubFeedUserStore{selector: userA.CalendarFeedSelector, user: userA}
	svc := NewCalendarFeedService(users, days, stubFeedDisclaimer{text: "d"}, []byte(calendarFeedTestSecretKey))

	// A's token resolves A and reads A's logs only.
	if _, ok, err := svc.ResolveFeed(context.Background(), tokenA, mustParseDashboardDay(t, "2026-03-20"), time.UTC); err != nil || !ok {
		t.Fatalf("A's token should resolve A: ok=%v err=%v", ok, err)
	}
	if days.requestedUser != 100 {
		t.Fatalf("A's feed must read only A's logs (id 100), got %d", days.requestedUser)
	}

	// B's token (B is NOT armed in this reader) must not resolve to A, and must
	// not trigger a scoped read for A. It is an ordinary 404 (no oracle).
	days.requestedCount = 0
	_, ok, err := svc.ResolveFeed(context.Background(), tokenB, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err != nil {
		t.Fatalf("unexpected error for B's token: %v", err)
	}
	if ok {
		t.Fatalf("B's token must NOT resolve to A's feed")
	}
	if days.requestedCount != 0 {
		t.Fatalf("a non-resolving token must not trigger any owner-scoped log read, got %d reads", days.requestedCount)
	}
}

// TestResolveFeedIdenticalNotFoundForEveryBadToken proves the no-oracle
// contract: malformed token, unknown selector, and wrong verifier all yield the
// identical (nil, false, nil) result — no body, no distinguishing error.
//
// That external identity alone would also hold if wrongVerifier's selector
// never resolved — the same "selector not found" branch unknownSelector takes
// — so each case also asserts the internal signal that tells the branches
// apart: the selector-lookup call count (malformed never reaches it) and
// whether the miss-path timing equalization ran (only a selector MISS pays it;
// VerifyCalendarFeedToken decides directly once a row resolves — see
// ResolveFeed). wrongVerifier hits the lookup and skips equalization, proving
// it actually reached the verifier compare rather than a second
// unknown-selector miss.
func TestResolveFeedIdenticalNotFoundForEveryBadToken(t *testing.T) {
	original := equalizeCalendarFeedTiming
	var equalizeCalls int
	equalizeCalendarFeedTiming = func([]byte, string, string) { equalizeCalls++ }
	t.Cleanup(func() { equalizeCalendarFeedTiming = original })

	user, validToken := armedFeedUser(t, 7, "2026-03-02")
	svc, users, _ := newFeedServiceForTest(user, predictableFeedLogs(t))
	now := mustParseDashboardDay(t, "2026-03-20")

	// Malformed: wrong length, rejected before any lookup.
	malformed := "TOOSHORT"
	selector, verifier := mustSplitFeedToken(t, validToken)
	// Unknown selector: right length shape, but selector not armed. Flip the
	// selector half of the valid token so the length stays valid.
	unknownSelector := strings.Repeat("Z", len(selector)) + verifier
	// Wrong verifier: correct selector, corrupted verifier half.
	wrongVerifier := selector + strings.Repeat("2", len(verifier))

	for name, tc := range map[string]struct {
		token        string
		wantLookups  int
		wantEqualize int
	}{
		"malformed":       {token: malformed, wantLookups: 0, wantEqualize: 0},
		"unknownSelector": {token: unknownSelector, wantLookups: 1, wantEqualize: 1},
		"wrongVerifier":   {token: wrongVerifier, wantLookups: 1, wantEqualize: 0},
	} {
		lookupsBefore := users.calls
		equalizeCalls = 0

		body, ok, err := svc.ResolveFeed(context.Background(), tc.token, now, time.UTC)
		if err != nil {
			t.Fatalf("%s: expected nil error (no oracle), got %v", name, err)
		}
		if ok {
			t.Fatalf("%s: expected ok=false", name)
		}
		if body != nil {
			t.Fatalf("%s: expected no body, got %d bytes", name, len(body))
		}
		if got := users.calls - lookupsBefore; got != tc.wantLookups {
			t.Fatalf("%s: expected %d selector lookup(s), got %d", name, tc.wantLookups, got)
		}
		if equalizeCalls != tc.wantEqualize {
			t.Fatalf("%s: expected %d timing-equalization call(s) (1 = selector miss, 0 = row found and refused by the real verifier compare), got %d",
				name, tc.wantEqualize, equalizeCalls)
		}
	}
}

// TestResolveFeedEqualizesTimingOnSelectorMiss asserts (via a call-counter, not
// wall-clock) that the selector-miss path performs a dummy verifier compare, so
// an unknown selector is timing-indistinguishable from a known selector with a
// bad verifier. Mirrors the equalizeAuthCredentialsTiming test idiom.
func TestResolveFeedEqualizesTimingOnSelectorMiss(t *testing.T) {
	original := equalizeCalendarFeedTiming
	calls := 0
	equalizeCalendarFeedTiming = func([]byte, string, string) { calls++ }
	t.Cleanup(func() { equalizeCalendarFeedTiming = original })

	user, validToken := armedFeedUser(t, 7, "2026-03-02")
	svc, _, _ := newFeedServiceForTest(user, predictableFeedLogs(t))
	now := mustParseDashboardDay(t, "2026-03-20")

	// A well-formed token whose selector resolves no row (selector-miss path).
	selector, verifier := mustSplitFeedToken(t, validToken)
	unknownSelector := strings.Repeat("Z", len(selector)) + verifier
	if _, ok, _ := svc.ResolveFeed(context.Background(), unknownSelector, now, time.UTC); ok {
		t.Fatalf("expected selector miss to be ok=false")
	}
	if calls != 1 {
		t.Fatalf("expected exactly one dummy verifier compare on the selector-miss path, got %d", calls)
	}

	// A malformed token is rejected before the lookup and must NOT spend the
	// equalization compare (it reveals nothing about selector existence).
	calls = 0
	if _, ok, _ := svc.ResolveFeed(context.Background(), "SHORT", now, time.UTC); ok {
		t.Fatalf("expected malformed token to be ok=false")
	}
	if calls != 0 {
		t.Fatalf("malformed token must not spend the equalization compare, got %d", calls)
	}
}

// TestCalendarFeedTimingEqualizationPlaceholderReachesTheVerifierCompare is the
// compatibility half of the call-counter test above: the counter proves the dummy
// compare HAPPENS, this proves it does real work.
//
// The equalization re-executes VerifyCalendarFeedToken rather than hardcoding a
// primitive, so it cannot drift from the verify path. What it CAN do is go free:
// the stand-in row carries no bcrypt hash, so an empty or hash-shaped placeholder
// MAC would fall through to the "feed off" refusal and skip every compare, leaving
// the enumeration oracle wide open with the counter test still green. This pins
// the placeholder against that.
//
// It is a structural pin rather than a latency measurement — wall-clock budget
// assertions are banned in this package.
func TestCalendarFeedTimingEqualizationPlaceholderReachesTheVerifierCompare(t *testing.T) {
	realMAC, err := security.CalendarFeedVerifierMAC([]byte(calendarFeedTestSecretKey), "SELECTOR16CHARSX", "VERIFIER")
	if err != nil {
		t.Fatalf("CalendarFeedVerifierMAC: %v", err)
	}
	if calendarFeedMACTimingEqualizationValue == "" {
		t.Fatal("an empty equalization placeholder makes the dummy compare free: verification refuses a row with no verifier column before doing any work")
	}
	if len(calendarFeedMACTimingEqualizationValue) != len(realMAC) {
		t.Fatalf("equalization placeholder is %d chars, a real verifier MAC is %d — the dummy compare must have the same width",
			len(calendarFeedMACTimingEqualizationValue), len(realMAC))
	}
	if strings.HasPrefix(calendarFeedMACTimingEqualizationValue, "$2") {
		t.Fatalf("equalization placeholder is a bcrypt hash (%q): the stand-in row it is compared against carries no bcrypt column, so it must be a MAC",
			calendarFeedMACTimingEqualizationValue)
	}
	// The placeholder can never authenticate a real pair, so the equalized path
	// cannot accidentally report a match.
	if security.VerifyCalendarFeedVerifierMAC([]byte(calendarFeedTestSecretKey), "SELECTOR16CHARSX", "VERIFIER", calendarFeedMACTimingEqualizationValue) {
		t.Fatalf("the equalization placeholder must never verify as a real MAC")
	}
}

// TestResolveFeedVerifiesThroughMACNotBcrypt proves the stored MAC is what
// decides for a row minted after migration 032: the bcrypt column is corrupted to
// a value that could never verify, and the feed still resolves. If verification
// ever fell back to (or preferred) bcrypt, this would 404.
func TestResolveFeedVerifiesThroughMACNotBcrypt(t *testing.T) {
	user, token := armedFeedUser(t, 11, "2026-03-02")
	user.CalendarFeedVerifierHash = "$2a$12$not.a.hash.that.could.ever.match.this.verifier.value.xxxxx"
	svc, users, _ := newFeedServiceForTest(user, predictableFeedLogs(t))

	_, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err != nil || !ok {
		t.Fatalf("a row carrying a valid MAC must verify without bcrypt: ok=%v err=%v", ok, err)
	}
	if users.backfillCalls != 0 {
		t.Fatalf("a row that already carries a MAC must never be backfilled, got %d writes", users.backfillCalls)
	}
}

// TestResolveFeedRefusesStaleMACWithoutBcryptFallback pins the deliberate hard
// refusal on a MAC mismatch, the shape a SECRET_KEY rotation takes: the row's MAC
// was minted under a different key while its bcrypt hash is still perfectly
// valid. Verification must NOT heal the row from bcrypt.
//
// Healing would keep a long-lived bearer capability alive across a key rotation
// AND would put every wrong verifier back on the bcrypt path, restoring the CPU
// cost this design removes. The documented consequence is that a rotation disarms
// armed feeds and the owner re-generates the subscribe URL from settings.
func TestResolveFeedRefusesStaleMACWithoutBcryptFallback(t *testing.T) {
	user, token := armedFeedUser(t, 12, "2026-03-02")
	selector, verifier := mustSplitFeedToken(t, token)
	staleMAC, err := security.CalendarFeedVerifierMAC([]byte(calendarFeedRotatedTestKey), selector, verifier)
	if err != nil {
		t.Fatalf("CalendarFeedVerifierMAC: %v", err)
	}
	user.CalendarFeedVerifierMAC = staleMAC

	svc, users, days := newFeedServiceForTest(user, predictableFeedLogs(t))
	body, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err != nil {
		t.Fatalf("a stale MAC is an ordinary not-found, not an error: %v", err)
	}
	if ok || body != nil {
		t.Fatalf("a MAC minted under a rotated key must be refused, not healed from the bcrypt hash")
	}
	if days.requestedCount != 0 {
		t.Fatalf("a refused token must not trigger an owner-scoped log read, got %d", days.requestedCount)
	}
	if users.backfillCalls != 0 {
		t.Fatalf("a mismatched MAC must never be overwritten by the backfill path, got %d writes", users.backfillCalls)
	}
}

// TestResolveFeedBackfillsMACForPre032RowAndLeavesBcryptBehind covers the lazy
// migration end to end: a row stored before migration 032 verifies through
// bcrypt, the MAC is written in during that same request (scoped to the resolved
// owner and the verified selector), and the next poll verifies through the MAC —
// proven by corrupting the bcrypt column before the second call.
func TestResolveFeedBackfillsMACForPre032RowAndLeavesBcryptBehind(t *testing.T) {
	user, token := armedFeedUser(t, 13, "2026-03-02")
	expectedMAC := user.CalendarFeedVerifierMAC
	// The same row as it was stored before migration 032: hash present, MAC absent.
	legacy := user
	legacy.CalendarFeedVerifierMAC = ""

	svc, users, _ := newFeedServiceForTest(legacy, predictableFeedLogs(t))
	now := mustParseDashboardDay(t, "2026-03-20")

	if _, ok, err := svc.ResolveFeed(context.Background(), token, now, time.UTC); err != nil || !ok {
		t.Fatalf("a pre-032 row must still verify through bcrypt: ok=%v err=%v", ok, err)
	}
	if users.backfillCalls != 1 {
		t.Fatalf("expected exactly one MAC backfill on the first successful poll, got %d", users.backfillCalls)
	}
	if users.backfilledUserID != 13 {
		t.Fatalf("backfill must be scoped to the resolved owner 13, got %d", users.backfilledUserID)
	}
	if users.backfilledSel != user.CalendarFeedSelector {
		t.Fatalf("backfill must be scoped to the verified selector %q, got %q", user.CalendarFeedSelector, users.backfilledSel)
	}
	if users.backfilledMAC != expectedMAC {
		t.Fatalf("backfilled MAC %q does not match the MAC generation derives for the same token (%q)", users.backfilledMAC, expectedMAC)
	}

	// The row now carries a MAC. Corrupt the bcrypt column: a second poll must
	// still resolve, proving the row left the slow path for good, and must not
	// write the MAC again.
	users.user.CalendarFeedVerifierHash = "$2a$12$not.a.hash.that.could.ever.match.this.verifier.value.xxxxx"
	if _, ok, err := svc.ResolveFeed(context.Background(), token, now, time.UTC); err != nil || !ok {
		t.Fatalf("the migrated row must verify through its fresh MAC: ok=%v err=%v", ok, err)
	}
	if users.backfillCalls != 1 {
		t.Fatalf("the MAC must be written once, not on every poll: got %d writes", users.backfillCalls)
	}
}

// TestResolveFeedSkipsMACBackfillWithoutSecretKey covers the other way the
// backfill can decline: with no secret key there is no MAC to derive. The pre-032
// bcrypt path needs no key, so the feed must still serve — it simply stays on the
// slow path instead of writing a MAC nobody could verify.
func TestResolveFeedSkipsMACBackfillWithoutSecretKey(t *testing.T) {
	legacy, token := legacyArmedFeedUser(t, 15, "2026-03-02")
	users := &stubFeedUserStore{selector: legacy.CalendarFeedSelector, user: legacy}
	days := &stubFeedDayReader{logs: predictableFeedLogs(t)}
	svc := NewCalendarFeedService(users, days, stubFeedDisclaimer{text: "d"}, nil)

	body, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err != nil || !ok {
		t.Fatalf("a pre-032 row must verify through bcrypt with no secret key: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(string(body), "BEGIN:VCALENDAR") {
		t.Fatal("expected the feed body to render")
	}
	if users.backfillCalls != 0 {
		t.Fatalf("with no key there is no MAC to store: expected no backfill write, got %d", users.backfillCalls)
	}
}

// TestResolveFeedServesFeedWhenMACBackfillFails proves the backfill is an
// optimization, never a gate: the token verified, so a failing CAS write (the
// owner rotated or revoked underneath the request, or the DB refused) must not
// turn a valid feed into a 404 or a 500.
func TestResolveFeedServesFeedWhenMACBackfillFails(t *testing.T) {
	legacy, token := legacyArmedFeedUser(t, 14, "2026-03-02")
	svc, users, _ := newFeedServiceForTest(legacy, predictableFeedLogs(t))
	users.backfillErr = errors.New("feed rotated underneath the request")

	body, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err != nil {
		t.Fatalf("a failed MAC backfill must not surface as an infrastructure error: %v", err)
	}
	if !ok || !strings.Contains(string(body), "BEGIN:VCALENDAR") {
		t.Fatalf("a failed MAC backfill must not withhold a feed whose token verified")
	}
}

func TestResolveFeedPropagatesInfrastructureError(t *testing.T) {
	user, token := armedFeedUser(t, 7, "2026-03-02")
	users := &stubFeedUserStore{selector: user.CalendarFeedSelector, user: user, err: errors.New("db down")}
	days := &stubFeedDayReader{}
	svc := NewCalendarFeedService(users, days, stubFeedDisclaimer{text: "d"}, []byte(calendarFeedTestSecretKey))

	_, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err == nil {
		t.Fatalf("expected the infrastructure error to propagate")
	}
	if ok {
		t.Fatalf("expected ok=false on infrastructure error")
	}
}

// TestResolveFeedPropagatesDayReadErrorAfterVerification drives the day-read
// error branch that sits AFTER a successful token verification: the token is
// valid (so the user lookup + verifier pass), but the owner-scoped log read
// fails. This is distinct from the user-lookup failure above, which returns
// before any log read.
func TestResolveFeedPropagatesDayReadErrorAfterVerification(t *testing.T) {
	user, token := armedFeedUser(t, 7, "2026-03-02")
	users := &stubFeedUserStore{selector: user.CalendarFeedSelector, user: user}
	days := &stubFeedDayReader{err: errors.New("log read failed")}
	svc := NewCalendarFeedService(users, days, stubFeedDisclaimer{text: "d"}, []byte(calendarFeedTestSecretKey))

	body, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err == nil {
		t.Fatalf("expected the day-read error to propagate after verification")
	}
	if ok {
		t.Fatalf("expected ok=false when the owner-scoped log read fails")
	}
	if body != nil {
		t.Fatalf("expected no body on a day-read failure, got %d bytes", len(body))
	}
	if days.requestedUser != 7 {
		t.Fatalf("expected the failing log read to be scoped to the resolved owner 7, got %d", days.requestedUser)
	}
}

// TestResolveFeedDefaultsToUTCWhenLocationNil drives the nil-location fallback:
// a cookieless feed request can arrive with no resolved timezone, so ResolveFeed
// must default to UTC rather than nil-deref. The valid token still resolves and
// the feed still renders.
func TestResolveFeedDefaultsToUTCWhenLocationNil(t *testing.T) {
	user, token := armedFeedUser(t, 7, "2026-03-02")
	svc, _, days := newFeedServiceForTest(user, predictableFeedLogs(t))

	body, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), nil)
	if err != nil {
		t.Fatalf("unexpected error with nil location: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok for a valid token with nil location")
	}
	if days.requestedCount != 1 {
		t.Fatalf("expected exactly one owner-scoped log read, got %d", days.requestedCount)
	}
	if !strings.Contains(string(body), "BEGIN:VCALENDAR") {
		t.Fatalf("expected a well-formed feed with nil location, got:\n%s", string(body))
	}
}

// TestGenerateCalendarFeedTokenVerifierIsRealBcrypt keeps the bcrypt column
// honest even though it no longer decides verification: it is what a rollback to a
// binary predating migration 032 reads, so a freshly minted token must verify
// against the stored hash ALONE, and a tampered verifier must not.
//
// The MAC is stripped from the stored triple here on purpose — that is exactly the
// view the older binary has of a row this one wrote.
func TestGenerateCalendarFeedTokenVerifierIsRealBcrypt(t *testing.T) {
	token, columns, err := GenerateCalendarFeedToken([]byte(calendarFeedTestSecretKey))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	if !strings.HasPrefix(columns.VerifierHash, "$2") {
		t.Fatalf("expected a real bcrypt hash for the rollback path, got %q", columns.VerifierHash)
	}
	rollbackView := models.CalendarFeedTokenColumns{Selector: columns.Selector, VerifierHash: columns.VerifierHash}
	if !VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), token, rollbackView) {
		t.Fatalf("a freshly minted token must verify against its stored bcrypt hash alone (rollback path)")
	}
	selector, verifier := mustSplitFeedToken(t, token)
	tampered := selector + strings.Repeat("2", len(verifier))
	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), tampered, rollbackView) {
		t.Fatalf("a tampered verifier must not verify")
	}
}
