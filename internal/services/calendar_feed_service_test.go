package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// stubFeedUserReader resolves a single armed owner by selector. Any other
// selector (or the empty string) is a miss, mirroring FindByCalendarFeedSelector.
type stubFeedUserReader struct {
	selector string
	user     models.User
	err      error
	calls    int
}

func (s *stubFeedUserReader) FindByCalendarFeedSelector(_ context.Context, selector string) (models.User, bool, error) {
	s.calls++
	if s.err != nil {
		return models.User{}, false, s.err
	}
	if selector != "" && selector == s.selector {
		return s.user, true, nil
	}
	return models.User{}, false, nil
}

// stubFeedDayReader records the userID it was asked for so a test can prove the
// feed scopes its log read to the resolved owner.
type stubFeedDayReader struct {
	logs           []models.DailyLog
	err            error
	requestedUser  uint
	requestedCount int
}

func (s *stubFeedDayReader) FetchLogsForUser(_ context.Context, userID uint, _ time.Time, _ time.Time, _ *time.Location) ([]models.DailyLog, error) {
	s.requestedUser = userID
	s.requestedCount++
	if s.err != nil {
		return nil, s.err
	}
	return s.logs, nil
}

type stubFeedDisclaimer struct{ text string }

func (s stubFeedDisclaimer) Disclaimer(string) string { return s.text }

// armedFeedUser mints a real token and returns the owner row that would resolve
// it, plus the full token to present. A real GenerateCalendarFeedToken keeps the
// verifier bcrypt path exercised end-to-end.
func armedFeedUser(t *testing.T, id uint, lastPeriodStart string) (models.User, string) {
	t.Helper()
	token, selector, verifierHash, err := GenerateCalendarFeedToken()
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
		CalendarFeedSelector:     selector,
		CalendarFeedVerifierHash: verifierHash,
	}
	return user, token
}

func newFeedServiceForTest(user models.User, logs []models.DailyLog) (*CalendarFeedService, *stubFeedUserReader, *stubFeedDayReader) {
	users := &stubFeedUserReader{selector: user.CalendarFeedSelector, user: user}
	days := &stubFeedDayReader{logs: logs}
	svc := NewCalendarFeedService(users, days, stubFeedDisclaimer{text: "estimates disclaimer"})
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
	users := &stubFeedUserReader{selector: userA.CalendarFeedSelector, user: userA}
	svc := NewCalendarFeedService(users, days, stubFeedDisclaimer{text: "d"})

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
// wall-clock) that the selector-miss path performs a dummy bcrypt, so an unknown
// selector is timing-indistinguishable from a known selector with a bad
// verifier. Mirrors the equalizeAuthCredentialsTiming test idiom.
func TestResolveFeedEqualizesTimingOnSelectorMiss(t *testing.T) {
	original := equalizeCalendarFeedTiming
	calls := 0
	equalizeCalendarFeedTiming = func(string) { calls++ }
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
		t.Fatalf("expected exactly one dummy bcrypt on the selector-miss path, got %d", calls)
	}

	// A malformed token is rejected before the lookup and must NOT spend the
	// equalization bcrypt (it reveals nothing about selector existence).
	calls = 0
	if _, ok, _ := svc.ResolveFeed(context.Background(), "SHORT", now, time.UTC); ok {
		t.Fatalf("expected malformed token to be ok=false")
	}
	if calls != 0 {
		t.Fatalf("malformed token must not spend the equalization bcrypt, got %d", calls)
	}
}

func TestResolveFeedPropagatesInfrastructureError(t *testing.T) {
	user, token := armedFeedUser(t, 7, "2026-03-02")
	users := &stubFeedUserReader{selector: user.CalendarFeedSelector, user: user, err: errors.New("db down")}
	days := &stubFeedDayReader{}
	svc := NewCalendarFeedService(users, days, stubFeedDisclaimer{text: "d"})

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
	users := &stubFeedUserReader{selector: user.CalendarFeedSelector, user: user}
	days := &stubFeedDayReader{err: errors.New("log read failed")}
	svc := NewCalendarFeedService(users, days, stubFeedDisclaimer{text: "d"})

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

// TestGenerateCalendarFeedTokenVerifierIsRealBcrypt pairs the timing-equalization
// call-counter test with a real bcrypt compatibility check: a freshly generated
// token verifies against its stored hash, and a tampered verifier does not. This
// guards that the equalization placeholder stays cost-compatible with the real
// verification path.
func TestGenerateCalendarFeedTokenVerifierIsRealBcrypt(t *testing.T) {
	token, selector, verifierHash, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	if !VerifyCalendarFeedToken(token, selector, verifierHash) {
		t.Fatalf("a freshly minted token must verify against its stored hash")
	}
	tampered := token[:16] + strings.Repeat("2", len(token)-16)
	if VerifyCalendarFeedToken(tampered, selector, verifierHash) {
		t.Fatalf("a tampered verifier must not verify")
	}
}
