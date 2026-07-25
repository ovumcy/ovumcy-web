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
// (the owner's "today") so a test can prove that today is resolved in the
// request timezone, not UTC.
type stubFeedDayReader struct {
	logs           []models.DailyLog
	err            error
	requestedUser  uint
	requestedTo    time.Time
	requestedCount int
}

func (s *stubFeedDayReader) FetchLogsForUser(_ context.Context, userID uint, _ time.Time, to time.Time, _ *time.Location) ([]models.DailyLog, error) {
	s.requestedUser = userID
	s.requestedTo = to
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

// TestResolveFeedResolvesTodayInRequestTimezone proves the log-read window's
// upper bound (the owner's "today") is computed in the request timezone, not
// UTC — so the feed reflects the owner's local calendar day. now is 23:00 UTC,
// already the next calendar day in a UTC+3 zone, so today must be that next day.
// Kills the calendar_feed_service.go:106 nil-location guard mutant, which would
// substitute UTC and read the wrong day's window.
func TestResolveFeedResolvesTodayInRequestTimezone(t *testing.T) {
	user, token := armedFeedUser(t, 51, "2026-03-02")
	svc, _, days := newFeedServiceForTest(user, predictableFeedLogs(t))

	// 2026-03-10 23:00 UTC == 2026-03-11 02:00 in UTC+3.
	loc := time.FixedZone("test+3", 3*60*60)
	now := time.Date(2026, time.March, 10, 23, 0, 0, 0, time.UTC)

	if _, ok, err := svc.ResolveFeed(context.Background(), token, now, loc); err != nil || !ok {
		t.Fatalf("expected the feed to resolve: ok=%v err=%v", ok, err)
	}
	if y, m, d := days.requestedTo.Date(); y != 2026 || m != time.March || d != 11 {
		t.Fatalf("today must be resolved in the request timezone (want 2026-03-11), got %04d-%02d-%02d", y, m, d)
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
func TestResolveFeedIdenticalNotFoundForEveryBadToken(t *testing.T) {
	user, validToken := armedFeedUser(t, 7, "2026-03-02")
	svc, _, _ := newFeedServiceForTest(user, predictableFeedLogs(t))
	now := mustParseDashboardDay(t, "2026-03-20")

	// Malformed: wrong length, rejected before any lookup.
	malformed := "TOOSHORT"
	// Unknown selector: right length shape, but selector not armed. Flip the
	// selector half of the valid token so the length stays valid.
	unknownSelector := "ZZZZZZZZZZZZZZZZ" + validToken[16:]
	// Wrong verifier: correct selector, corrupted verifier half.
	wrongVerifier := validToken[:16] + strings.Repeat("2", len(validToken)-16)

	for name, bad := range map[string]string{
		"malformed":       malformed,
		"unknownSelector": unknownSelector,
		"wrongVerifier":   wrongVerifier,
	} {
		body, ok, err := svc.ResolveFeed(context.Background(), bad, now, time.UTC)
		if err != nil {
			t.Fatalf("%s: expected nil error (no oracle), got %v", name, err)
		}
		if ok {
			t.Fatalf("%s: expected ok=false", name)
		}
		if body != nil {
			t.Fatalf("%s: expected no body, got %d bytes", name, len(body))
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
	unknownSelector := "ZZZZZZZZZZZZZZZZ" + validToken[16:]
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
	selector, verifier, _ := SplitCalendarFeedToken(token)
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
	tampered := token[:16] + strings.Repeat("2", len(token)-16)
	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), tampered, rollbackView) {
		t.Fatalf("a tampered verifier must not verify")
	}
}
