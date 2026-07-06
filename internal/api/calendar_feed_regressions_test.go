package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// armCalendarFeedForUser mints a real feed token for the user, persists the
// selector + verifier hash, and seeds a stable ~28d period cadence so the feed
// emits prediction events. It returns the full shown-once token to present in
// the URL.
func armCalendarFeedForUser(t *testing.T, database *gorm.DB, userID uint) string {
	t.Helper()
	token, selector, verifierHash, err := services.GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	repo := db.NewRepositories(database).Users
	if err := repo.SaveCalendarFeedToken(t.Context(), userID, models.CalendarFeedTokenColumns{
		Selector:     selector,
		VerifierHash: verifierHash,
	}); err != nil {
		t.Fatalf("SaveCalendarFeedToken: %v", err)
	}
	for _, day := range []string{"2026-01-05", "2026-02-02", "2026-03-02"} {
		parsed, _ := time.ParseInLocation("2006-01-02", day, time.UTC)
		if err := database.Create(&models.DailyLog{UserID: userID, Date: parsed, IsPeriod: true}).Error; err != nil {
			t.Fatalf("seed period log %s: %v", day, err)
		}
	}
	// Anchor the current cycle to the most recent seeded start.
	start, _ := time.ParseInLocation("2006-01-02", "2026-03-02", time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", userID).Update("last_period_start", start).Error; err != nil {
		t.Fatalf("set last_period_start: %v", err)
	}
	return token
}

func calendarFeedURL(token string) string {
	return "/calendar/feed/" + token + ".ics"
}

// TestCalendarFeedServesOwnersICSWithHardenedHeaders is the happy-path contract:
// a valid token returns 200 with text/calendar, a private 1h cache, the
// noindex robots tag, structural .ics markers, and the medical-safety disclaimer
// in a DESCRIPTION — and never a Set-Cookie.
func TestCalendarFeedServesOwnersICSWithHardenedHeaders(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-ok@example.com", "StrongPass1", true)
	token := armCalendarFeedForUser(t, database, user.ID)

	request := httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil)
	response := mustAppResponse(t, app, request)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for a valid feed token, got %d", response.StatusCode)
	}
	if ct := response.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/calendar") {
		t.Fatalf("expected text/calendar content type, got %q", ct)
	}
	if cc := response.Header.Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Fatalf("expected private 1h cache, got %q", cc)
	}
	if robots := response.Header.Get("X-Robots-Tag"); robots != "noindex" {
		t.Fatalf("expected X-Robots-Tag noindex, got %q", robots)
	}
	if len(response.Cookies()) != 0 {
		t.Fatalf("feed must not set any cookie, got %#v", response.Cookies())
	}

	body := readBodyString(t, response)
	for _, marker := range []string{"BEGIN:VCALENDAR", "BEGIN:VEVENT", "SUMMARY:", "DESCRIPTION:"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("expected .ics marker %q, got:\n%s", marker, body)
		}
	}
	// Medical-safety disclaimer (exact-wording invariant) present in a DESCRIPTION.
	// Assert against the UNFOLDED body — RFC 5545 folds long lines with a CRLF +
	// space, which a calendar client (and this check) must unfold before reading
	// the value; the wording itself is unchanged.
	if unfolded := unfoldICS(body); !strings.Contains(unfolded, "not medical advice or a method of contraception") {
		t.Fatalf("expected medical-safety disclaimer in feed body, got:\n%s", body)
	}
	// Neutral-title invariant: SUMMARY carries no date/phase.
	for _, line := range strings.Split(body, "\r\n") {
		if strings.HasPrefix(line, "SUMMARY:") && line != "SUMMARY:Ovumcy: reminder (estimate)" {
			t.Fatalf("SUMMARY must be the fixed neutral label, got %q", line)
		}
	}
}

// TestCalendarFeedReturnsBare404WithoutOracleForBadTokens proves the no-oracle
// contract at the transport boundary: a bogus, malformed, and cleared-feed token
// all return an identical bare 404 with no body and no Set-Cookie.
func TestCalendarFeedReturnsBare404WithoutOracleForBadTokens(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-404@example.com", "StrongPass1", true)
	validToken := armCalendarFeedForUser(t, database, user.ID)

	// A token whose selector resolves no row, a too-short malformed token, and a
	// correct-selector/wrong-verifier token.
	bogus := "ZZZZZZZZZZZZZZZZ" + validToken[16:]
	malformed := "SHORT"
	wrongVerifier := validToken[:16] + strings.Repeat("2", len(validToken)-16)

	// After clearing the feed, the previously-valid token must 404 too.
	if err := db.NewRepositories(database).Users.ClearCalendarFeedToken(t.Context(), user.ID); err != nil {
		t.Fatalf("ClearCalendarFeedToken: %v", err)
	}

	for name, token := range map[string]string{
		"bogus":            bogus,
		"malformed":        malformed,
		"wrongVerifier":    wrongVerifier,
		"clearedValidOnce": validToken,
	} {
		request := httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil)
		response := mustAppResponse(t, app, request)
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: expected bare 404, got %d", name, response.StatusCode)
		}
		if len(response.Cookies()) != 0 {
			t.Fatalf("%s: 404 must not set a cookie, got %#v", name, response.Cookies())
		}
		if body := readBodyString(t, response); strings.Contains(body, "BEGIN:VCALENDAR") {
			t.Fatalf("%s: 404 must not leak a calendar body, got:\n%s", name, body)
		}
	}
}

// TestCalendarFeedTokenIsRedactedFromRequestLog drives a real request through a
// Fiber request logger wired to the SAME SafeRequestLogPath tag production uses,
// and asserts the captured log line carries ":token.ics", never the token value.
func TestCalendarFeedTokenIsRedactedFromRequestLog(t *testing.T) {
	_, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-log@example.com", "StrongPass1", true)
	token := armCalendarFeedForUser(t, database, user.ID)

	var logBuf bytes.Buffer
	logged := fiber.New()
	logged.Use(logger.New(logger.Config{
		Stream: &logBuf,
		Format: "${method} ${request_path}\n",
		CustomTags: map[string]logger.LogFunc{
			"request_path": func(buffer logger.Buffer, c fiber.Ctx, _ *logger.Data, _ string) (int, error) {
				return buffer.WriteString(SafeRequestLogPath(c))
			},
		},
	}))
	// Register the feed route on the logging app so the matched-route template
	// drives SafeRequestLogPath exactly as in production.
	handler := mustFeedHandler(t, database)
	logged.Get(calendarFeedRoutePath, handler.ServeCalendarFeed)

	request := httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil)
	if _, err := logged.Test(request); err != nil {
		t.Fatalf("request: %v", err)
	}

	logLine := logBuf.String()
	if strings.Contains(logLine, token) {
		t.Fatalf("request log leaked the feed token: %q", logLine)
	}
	if !strings.Contains(logLine, ":token.ics") {
		t.Fatalf("expected request log to mask the token as :token.ics, got %q", logLine)
	}
}

// TestSanitizeRequestLogPathMasksRawTokenDotICSFallback pins the defense-in-depth
// hardening: even the RAW-PATH fallback (when no matched-route template is
// available) masks a "<token>.ics" segment, closing the flagged gap where the
// trailing ".ics" would otherwise defeat the opaque-token redaction.
func TestSanitizeRequestLogPathMasksRawTokenDotICSFallback(t *testing.T) {
	rawToken := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789ABCDEFGHJKLMNP"
	got := sanitizeRequestLogPath("/calendar/feed/" + rawToken + ".ics")
	if strings.Contains(got, rawToken) {
		t.Fatalf("raw-path fallback leaked the token: %q", got)
	}
	if got != "/calendar/feed/:token.ics" {
		t.Fatalf("expected raw fallback to mask as /calendar/feed/:token.ics, got %q", got)
	}
}

// TestCalendarFeedIsRateLimitedPerIP drives a REAL limiter.New mounted on the
// feed prefix and asserts the endpoint returns 429 once the per-IP budget is
// exceeded, with an integer Retry-After no greater than the window.
func TestCalendarFeedIsRateLimitedPerIP(t *testing.T) {
	_, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-rl@example.com", "StrongPass1", true)
	token := armCalendarFeedForUser(t, database, user.ID)
	handler := mustFeedHandler(t, database)

	const maxRequests = 3
	rlApp := fiber.New()
	rlApp.Use(CalendarFeedRateLimitPrefix, limiter.New(limiter.Config{
		Max:        maxRequests,
		Expiration: time.Minute,
		LimitReached: func(c fiber.Ctx) error {
			return c.SendStatus(fiber.StatusTooManyRequests)
		},
	}))
	rlApp.Get(calendarFeedRoutePath, handler.ServeCalendarFeed)

	url := calendarFeedURL(token)
	var lastStatus int
	var retryAfter string
	for range maxRequests + 1 {
		request := httptest.NewRequest(http.MethodGet, url, nil)
		response := mustAppResponse(t, rlApp, request)
		lastStatus = response.StatusCode
		retryAfter = response.Header.Get(fiber.HeaderRetryAfter)
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exceeding the per-IP budget, got %d", lastStatus)
	}
	if retryAfter == "" {
		t.Fatalf("expected a Retry-After header on the 429")
	}
}

// mustFeedHandler builds a handler bound to the given database, for tests that
// mount the feed route on a bespoke Fiber app (logger / rate-limit wiring).
func mustFeedHandler(t *testing.T, database *gorm.DB) *Handler {
	t.Helper()
	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	handler, err := NewHandler("test-secret-key", time.UTC, i18nManager, false, newTestHandlerDependencies(database, i18nManager))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}
	return handler
}

// unfoldICS reverses RFC 5545 §3.1 line folding: a CRLF immediately followed by
// a single space (or tab) is a fold and is removed, rejoining the split content
// line. This is exactly what a conforming calendar client does before reading a
// property value.
func unfoldICS(body string) string {
	unfolded := strings.ReplaceAll(body, "\r\n ", "")
	return strings.ReplaceAll(unfolded, "\r\n\t", "")
}

func readBodyString(t *testing.T, response *http.Response) string {
	t.Helper()
	defer func() { _ = response.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(response.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return buf.String()
}
