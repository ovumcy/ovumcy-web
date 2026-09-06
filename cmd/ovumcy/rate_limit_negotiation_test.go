package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// rateLimitTestSecretKey is the app-level secret the test handler is built
// with (bootstrap.BuildDependencies): callers that need to mint a real
// calendar-feed token against this same handler's database (services.GenerateCalendarFeedToken)
// reuse this constant rather than a second hardcoded copy that could drift.
const rateLimitTestSecretKey = "test-secret-key"

func newRateLimitTestI18nManager(t *testing.T) *i18n.Manager {
	t.Helper()

	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("failed to initialize i18n manager for rate-limit tests: %v", err)
	}
	return manager
}

func newRateLimitTestHandler(t *testing.T) *api.Handler {
	t.Helper()

	handler, _ := newRateLimitTestHandlerAndDB(t)
	return handler
}

// newRateLimitTestHandlerAndDB is newRateLimitTestHandler plus the backing
// database, for callers that need to seed a row (e.g. arm a real calendar-feed
// token with services.GenerateCalendarFeedToken([]byte(rateLimitTestSecretKey)))
// against the exact handler under test rather than a disconnected database.
func newRateLimitTestHandlerAndDB(t *testing.T) (*api.Handler, *gorm.DB) {
	t.Helper()

	return newRateLimitTestHandlerAndDBAtLocation(t, time.UTC)
}

// newRateLimitTestHandlerAndDBAtLocation is newRateLimitTestHandlerAndDB with
// the handler's own instance zone parameterized instead of hardcoded to
// time.UTC — for a caller whose fixture depends on the instance zone being
// something OTHER than UTC (a request-timezone-signal probe needs a poller-
// claimed zone with a real offset gap from its baseline; UTC would need one
// exceeding fiber's own +14 ceiling, which no real zone reaches).
// runtimeConfig.Location — the argument newFiberApp otherwise takes — is
// composition-root config for session cookies and has no effect here:
// handler.location is fixed at construction, by api.NewHandler's own second
// argument, the moment this builds it.
func newRateLimitTestHandlerAndDBAtLocation(t *testing.T, location *time.Location) (*api.Handler, *gorm.DB) {
	t.Helper()

	tempDB, err := os.CreateTemp("", "ovumcy-rate-limit-*.db")
	if err != nil {
		t.Fatalf("create rate-limit test database path: %v", err)
	}
	if err := tempDB.Close(); err != nil {
		t.Fatalf("close temp database file: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(tempDB.Name())
	})

	database, err := db.OpenDatabase(db.Config{
		Driver:     db.DriverSQLite,
		SQLitePath: tempDB.Name(),
	})
	if err != nil {
		t.Fatalf("open rate-limit test database: %v", err)
	}

	i18nManager := newRateLimitTestI18nManager(t)

	handler, err := api.NewHandler(
		"0123456789abcdef0123456789abcdef",
		location,
		i18nManager,
		false,
		bootstrap.BuildDependencies(db.NewRepositories(database), []byte(rateLimitTestSecretKey), i18nManager, bootstrap.Options{
			RegistrationMode: services.RegistrationModeOpen,
			OIDCConfig:       security.OIDCConfig{},
			LoginAttempts:    bootstrap.AttemptLimit{Max: 8, Window: 15 * time.Minute},
			RecoveryAttempts: bootstrap.AttemptLimit{Max: 8, Window: time.Hour},
			LogoutAttempts:   &bootstrap.AttemptLimit{},
			AuditLogEnabled:  true,
		}),
	)
	if err != nil {
		t.Fatalf("init rate-limit test handler: %v", err)
	}
	return handler, database
}

func TestAuthRateLimitHandlerTreatsJSONContentTypeAsJSONRequest(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Post("/api/v1/sessions", newAuthRateLimitHandler(handler, authRateLimitConfig{
		ErrorCode: "too_many_login_attempts",
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"email":"rate-limit@example.com"}`))
	request.Header.Set("Content-Type", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("auth rate-limit request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
	if got, ok := payload["error"].(string); !ok || got != "too_many_login_attempts" {
		t.Fatalf("expected stable auth rate-limit key, got %#v", payload)
	}
}

func TestAuthRateLimitHandlerRedirectUsesSealedFlashCookie(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Post("/api/v1/sessions", newAuthRateLimitHandler(handler, authRateLimitConfig{
		ErrorCode: "too_many_login_attempts",
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader("email=rate-limit%40example.com"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("auth rate-limit redirect request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	flashCookie := testResponseCookie(response.Cookies(), "ovumcy_flash")
	if flashCookie == nil {
		t.Fatal("expected flash cookie in redirect response")
	}
	if strings.Contains(flashCookie.Value, "rate-limit@example.com") {
		t.Fatalf("did not expect sealed flash cookie to expose email in plaintext: %q", flashCookie.Value)
	}
}

func TestOIDCRateLimitHandlerRedirectUsesSealedFlashCookie(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get("/auth/oidc/start", newAuthRateLimitHandler(handler, authRateLimitConfig{
		ErrorCode: "too_many_sso_attempts",
	}))

	request := httptest.NewRequest(http.MethodGet, "/auth/oidc/start?error=access_denied", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("oidc rate-limit redirect request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	flashCookie := testResponseCookie(response.Cookies(), "ovumcy_flash")
	if flashCookie == nil {
		t.Fatal("expected flash cookie in redirect response")
	}
	if strings.Contains(flashCookie.Value, "access_denied") {
		t.Fatalf("did not expect sealed flash cookie to expose provider error in plaintext: %q", flashCookie.Value)
	}
}

func TestSettingsAPIRateLimitHandlerRedirectsToSettings(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Patch("/api/v1/users/current/profile", newAPIRateLimitHandler(handler))

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/profile", strings.NewReader("display_name=Owner"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings rate-limit request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", location)
	}
}

func TestAPIRateLimitHandlerReturnsStatusErrorMarkupForHTMX(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Post("/api/v1/stats/overview", newAPIRateLimitHandler(handler))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/stats/overview", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("api rate-limit htmx request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, `class="status-error"`) {
		t.Fatalf("expected status-error markup, got %q", rendered)
	}
	if !strings.Contains(rendered, "Too many requests.") {
		t.Fatalf("expected localized generic rate-limit message, got %q", rendered)
	}
}

// TestCalendarFeedLimiterUsesItsOwnBudgetNotTheAPIBudget pins the WIRING, not the
// config: it drives the real configureFiberMiddleware and proves the feed prefix
// is capped by CalendarFeedMax while /api still gets APIMax. The config-level
// test cannot catch a revert that leaves the new field in place but passes
// APIMax to the feed limiter — this one can, because the two budgets are set far
// apart and the feed must trip first.
func TestCalendarFeedLimiterUsesItsOwnBudgetNotTheAPIBudget(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	// fiberConfig, not a bare fiber.New(): the wiring under test is the shipped
	// app's, and the routing flags the shipped config carries are what decide
	// which spellings of a path reach a route at all (see
	// rate_limit_scope_guard_test.go).
	app := fiber.New(fiberConfig(proxySettings{}))
	configureFiberMiddleware(app, runtimeConfig{
		RateLimits: rateLimitSettings{
			APIMax:             100,
			APIWindow:          time.Minute,
			CalendarFeedMax:    2,
			CalendarFeedWindow: time.Minute,
		},
	}, handler)
	app.Get(api.CalendarFeedRateLimitPrefix+"/:token.ics", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/api/v1/ping", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	get := func(t *testing.T, target string) int {
		t.Helper()
		response, err := app.Test(httptest.NewRequest(http.MethodGet, target, nil), testConfigNoTimeout)
		if err != nil {
			t.Fatalf("request %s failed: %v", target, err)
		}
		defer func() { _ = response.Body.Close() }()
		return response.StatusCode
	}

	const feedTarget = api.CalendarFeedRateLimitPrefix + "/ABCDEFGHJKLMNPQRSTUVWXYZ23456789ABCDEFGHJKLMNP12.ics"
	for i := 1; i <= 2; i++ {
		if status := get(t, feedTarget); status != http.StatusNoContent {
			t.Fatalf("feed request %d within budget: got %d, want 204", i, status)
		}
	}
	if status := get(t, feedTarget); status != http.StatusTooManyRequests {
		t.Fatalf("feed request past its budget of 2: got %d, want 429 — the feed limiter is not using CalendarFeedMax", status)
	}

	// Control: the API budget is untouched by the feed's exhaustion, proving the
	// two limiters keep separate buckets rather than one shared budget.
	if status := get(t, "/api/v1/ping"); status != http.StatusNoContent {
		t.Fatalf("api request after the feed budget was spent: got %d, want 204", status)
	}
}

// TestLanguageSwitchIsRateLimited pins the cap on POST /lang. It is the one
// unauthenticated route outside /api that reads a request body, so the /api
// limiter's path prefix does not reach it and it was previously the only
// body-reading surface in the app with no volume control at all. CSRF keeps a
// cross-origin attacker out but is not a cap.
//
// The control at the end is what proves the two limiters keep separate buckets
// rather than one shared counter — without it, a passing 429 could equally mean
// the /api limiter had been consumed.
func TestLanguageSwitchIsRateLimited(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	// Built on the shipped fiberConfig for the reason above: the spelling
	// variants of /lang this test does not send are covered by the sweep in
	// rate_limit_scope_guard_test.go, and both need the shipped routing flags.
	app := fiber.New(fiberConfig(proxySettings{}))
	configureFiberMiddleware(app, runtimeConfig{
		RateLimits: rateLimitSettings{
			APIMax:    2,
			APIWindow: time.Minute,
		},
	}, handler)
	app.Post("/lang", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/lang", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/api/v1/ping", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	send := func(t *testing.T, method, target string) int {
		t.Helper()
		response, err := app.Test(httptest.NewRequest(method, target, nil), testConfigNoTimeout)
		if err != nil {
			t.Fatalf("%s %s failed: %v", method, target, err)
		}
		defer func() { _ = response.Body.Close() }()
		return response.StatusCode
	}

	// The limiter is registered ahead of the CSRF middleware, so a token-less
	// POST is counted and then refused by CSRF (403). That ordering is the point:
	// the cap has to bound requests that never reach the handler, which is
	// exactly what an unauthenticated flood looks like. Within budget the CSRF
	// refusal is what comes back; past it, the limiter answers first.
	for i := 1; i <= 2; i++ {
		if status := send(t, http.MethodPost, "/lang"); status != http.StatusForbidden {
			t.Fatalf("POST /lang request %d within budget: got %d, want the CSRF refusal 403", i, status)
		}
	}
	if status := send(t, http.MethodPost, "/lang"); status != http.StatusTooManyRequests {
		t.Fatalf("POST /lang past its budget of 2: got %d, want 429 — the language switch is uncapped", status)
	}

	// The limiter is scoped to the mutating method: reading a page must not be
	// refused because someone spent the switch budget.
	if status := send(t, http.MethodGet, "/lang"); status != http.StatusNoContent {
		t.Fatalf("GET /lang after the POST budget was spent: got %d, want 204", status)
	}
	// Control: the /api budget is untouched.
	if status := send(t, http.MethodGet, "/api/v1/ping"); status != http.StatusNoContent {
		t.Fatalf("api request after the /lang budget was spent: got %d, want 204", status)
	}
}

func TestRateLimiterRetryAfterHeaderDoesNotLeakTimerState(t *testing.T) {
	// Privacy invariant:
	// "Retry-After header on rate-limit responses MUST NOT expose precise
	// internal timer state that could be used as an oracle." The Fiber
	// limiter encodes a coarse integer second count bounded by the configured
	// window. This regression guards against accidental upgrades to high-
	// resolution timestamps or HTTP-date values that would expose monotonic
	// state.
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)

	const expirationSeconds = 30
	app.Use("/api/v1/sessions", limiter.New(limiter.Config{
		Max:        1,
		Expiration: expirationSeconds * time.Second,
		LimitReached: newAuthRateLimitHandler(handler, authRateLimitConfig{
			ErrorCode: "too_many_login_attempts",
		}),
	}))
	app.Post("/api/v1/sessions", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	// Burn the single allowed request.
	first := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", nil)
	first.Header.Set("Content-Type", "application/json")
	firstResponse, err := app.Test(first, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	defer func() { _ = firstResponse.Body.Close() }()
	if firstResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("expected first request to succeed (204), got %d", firstResponse.StatusCode)
	}

	// Second request must trip the limiter.
	second := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", nil)
	second.Header.Set("Content-Type", "application/json")
	secondResponse, err := app.Test(second, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer func() { _ = secondResponse.Body.Close() }()
	if secondResponse.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected second request to trip limiter (429), got %d", secondResponse.StatusCode)
	}

	retryAfter := strings.TrimSpace(secondResponse.Header.Get("Retry-After"))
	if retryAfter == "" {
		t.Fatal("expected Retry-After header on rate-limited response")
	}

	// Must be plain integer seconds, not an HTTP-date (which would leak wall-clock state).
	if strings.ContainsAny(retryAfter, ":,") {
		t.Fatalf("Retry-After must be integer seconds, not HTTP-date: %q", retryAfter)
	}
	seconds, parseErr := strconv.Atoi(retryAfter)
	if parseErr != nil {
		t.Fatalf("Retry-After must parse as integer seconds, got %q: %v", retryAfter, parseErr)
	}

	// Granularity must be 1 second (not millisecond or sub-second). Already
	// enforced by integer parsing above. Bounds: the value MUST NOT exceed
	// the configured window — a larger value would imply leakage of state
	// from outside the bucket. A zero/negative value would also be wrong.
	if seconds < 1 || seconds > expirationSeconds {
		t.Fatalf("Retry-After must fall inside (0, %ds] window, got %ds", expirationSeconds, seconds)
	}
}

func TestAPIRateLimitHandlerReturnsJSONForGenericBrowserRequests(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Put("/api/v1/days/2026-02-17", newAPIRateLimitHandler(handler))

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-17", strings.NewReader("notes=test"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("generic api rate-limit request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode generic rate-limit response: %v", err)
	}
	if got, ok := payload["error"].(string); !ok || got != "too many requests" {
		t.Fatalf("expected generic rate-limit error key, got %#v", payload)
	}
}

// TestCalendarFeedRateLimitHandlerAnswers429AndRedactsToken drives the
// calendar-feed LimitReached handler (newCalendarFeedRateLimitHandler). The
// feed has no UI, so the handler answers 429 through the shared limiter
// path — never a calendar body a client would try to parse; the body shape
// itself is owned by the envelope tests above. And because it logs the hit
// via logRateLimitHit → SafeRequestLogPath, the log line must carry the
// masked route template, never the token value.
func TestCalendarFeedRateLimitHandlerAnswers429AndRedactsToken(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	// Mount the production LimitReached handler directly on the feed route so a
	// single request exercises its full body (log + security event + status).
	app.Get(api.CalendarFeedRateLimitPrefix+"/:token.ics", newCalendarFeedRateLimitHandler(handler))

	const feedToken = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789ABCDEFGHJKLMNP"
	request := httptest.NewRequest(http.MethodGet, api.CalendarFeedRateLimitPrefix+"/"+feedToken+".ics", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar-feed rate-limit request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 from the feed rate-limit handler, got %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.Contains(string(body), "BEGIN:VCALENDAR") {
		t.Fatalf("429 must not carry a calendar body, got %q", string(body))
	}

	logLine := output.String()
	if strings.Contains(logLine, feedToken) {
		t.Fatalf("rate-limit log leaked the feed token: %q", logLine)
	}
	if !strings.Contains(logLine, ":token.ics") {
		t.Fatalf("expected masked route template in rate-limit log, got %q", logLine)
	}
}
