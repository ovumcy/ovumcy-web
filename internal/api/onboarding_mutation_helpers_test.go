package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Behavior-asserting tests closing mutation survivors in the onboarding input
// helpers (handlers_onboarding_input_helpers.go) and the auth-token TTL constants
// declared in handler_types.go. The timezone-parse helpers take a fiber.Ctx, so
// they are exercised through a minimal mounted route that echoes their result —
// this pins the branch decisions directly without depending on the full
// onboarding persistence flow (which masks these branches because its form body
// parses identically via both PostArgs and url.ParseQuery).

// mountTimezoneValueProbe returns a Fiber app whose POST /tz-probe echoes
// onboardingFormTimezoneValue(c) wrapped in brackets so an empty result is
// visible as "[]".
func mountTimezoneValueProbe() *fiber.App {
	app := fiber.New()
	app.Post("/tz-probe", func(c fiber.Ctx) error {
		return c.SendString("[" + onboardingFormTimezoneValue(c) + "]")
	})
	return app
}

func timezoneValueProbeResult(t *testing.T, app *fiber.App, contentType, body string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/tz-probe", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("tz-probe request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return mustReadBodyString(t, resp.Body)
}

// TestOnboardingFormTimezoneValue_PostArgPresentShortCircuitsBodyReparse pins the
// `raw != ""` decision (handlers_onboarding_input_helpers.go:18): when the
// client_timezone field is present in the parsed POST args, it is returned
// verbatim WITHOUT re-parsing the raw body. A body that url.ParseQuery cannot
// round-trip (contains ';') proves the short-circuit: the whole PostArg value is
// returned, whereas negating the guard to `== ""` drops into the body re-parse,
// which errors on ';' and yields "".
func TestOnboardingFormTimezoneValue_PostArgPresentShortCircuitsBodyReparse(t *testing.T) {
	app := mountTimezoneValueProbe()

	// application/x-www-form-urlencoded => fasthttp populates PostArgs; the ';'
	// is preserved in the PostArg value but makes url.ParseQuery fail.
	got := timezoneValueProbeResult(t, app, "application/x-www-form-urlencoded", "client_timezone=Zone-A;junk=1")
	if got != "[Zone-A;junk=1]" {
		t.Fatalf("present PostArg must be returned verbatim, got %q", got)
	}
}

// TestOnboardingFormTimezoneValue_FallsBackToBodyQuery pins BOTH the empty-PostArg
// fallthrough and the `err != nil` guard (handlers_onboarding_input_helpers.go:23).
// A non-form Content-Type leaves PostArgs empty, so the function must parse the
// raw body as a query string and return the field. Negating `err != nil` to
// `== nil` would return "" on the success path.
func TestOnboardingFormTimezoneValue_FallsBackToBodyQuery(t *testing.T) {
	app := mountTimezoneValueProbe()

	// text/plain => PostArgs empty => url.ParseQuery(body) path, which succeeds.
	got := timezoneValueProbeResult(t, app, "text/plain", "client_timezone=Zone-Body")
	if got != "[Zone-Body]" {
		t.Fatalf("body-query fallback must return the parsed value, got %q", got)
	}

	// A body url.ParseQuery cannot decode (bad percent-escape) hits the error
	// branch and yields "" — this is the true side of the `err != nil` guard.
	gotBad := timezoneValueProbeResult(t, app, "text/plain", "client_timezone=%zzBad")
	if gotBad != "[]" {
		t.Fatalf("unparseable body must yield empty timezone, got %q", gotBad)
	}
}

// mountOnboardingLocationProbe mounts a route that invokes
// requestLocationFromOnboardingForm directly on a minimal handler (no
// LanguageMiddleware). This isolates the handler's OWN timezone-cookie decision:
// in the full request pipeline LanguageMiddleware also mirrors the header into the
// cookie, which masks the handler-level branch, so a bare route is required to
// observe handlers_onboarding_input_helpers.go:31.
func mountOnboardingLocationProbe() *fiber.App {
	handler := &Handler{location: time.UTC}
	app := fiber.New()
	app.Post("/loc-probe", func(c fiber.Ctx) error {
		loc := handler.requestLocationFromOnboardingForm(c)
		return c.SendString(loc.String())
	})
	return app
}

// TestRequestLocationFromOnboardingForm_RefreshesTimezoneCookieOnMismatch pins the
// `strings.TrimSpace(c.Cookies(...)) != canonical` decision
// (handlers_onboarding_input_helpers.go:31). With a valid header timezone whose
// canonical form differs from the current cookie, the handler must (re)issue the
// timezone cookie; when the cookie already matches, it must NOT. Negating to `==`
// inverts both, which the two sub-cases below catch.
func TestRequestLocationFromOnboardingForm_RefreshesTimezoneCookieOnMismatch(t *testing.T) {
	app := mountOnboardingLocationProbe()

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)

	t.Run("mismatch sets cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/loc-probe", strings.NewReader(""))
		req.Header.Set(timezoneHeaderName, timezoneName) // valid header, NO tz cookie -> mismatch
		resp, err := app.Test(req, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("loc-probe request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if body := mustReadBodyString(t, resp.Body); body != location.String() {
			t.Fatalf("resolved location = %q, want %q", body, location.String())
		}
		cookie := responseCookie(resp.Cookies(), timezoneCookieName)
		if cookie == nil {
			t.Fatalf("expected %s cookie to be set on header/cookie mismatch", timezoneCookieName)
		}
		if strings.TrimSpace(cookie.Value) != timezoneName {
			t.Fatalf("expected %s cookie value %q, got %q", timezoneCookieName, timezoneName, cookie.Value)
		}
	})

	t.Run("already-matching cookie is not re-set", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/loc-probe", strings.NewReader(""))
		req.Header.Set(timezoneHeaderName, timezoneName)
		req.Header.Set("Cookie", timezoneCookieName+"="+timezoneName) // cookie already canonical
		resp, err := app.Test(req, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("loc-probe request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if cookie := responseCookie(resp.Cookies(), timezoneCookieName); cookie != nil {
			t.Fatalf("cookie already matched canonical %q; handler must not re-set it, got Set-Cookie %q", timezoneName, cookie.Value)
		}
	})
}

// TestOnboardingStep2_JSONBodyValidInputSucceeds pins the JSON-body error guard
// in parseOnboardingStep2Input (handlers_onboarding_input_helpers.go:117). On the
// JSON path the cycle/period ints are re-serialized with strconv.Itoa before
// ParseAndNormalizeStep2Input re-parses them, so that call never errors for a
// well-formed JSON body — meaning the `if err != nil` guard's false branch is the
// only reachable one. A valid JSON step-2 submission must therefore be accepted
// (200, not a validation error). Negating the guard to `== nil` makes every valid
// JSON step-2 body wrongly return "invalid input". No existing test posts a JSON
// step-2 body, so this branch was uncovered.
func TestOnboardingStep2_JSONBodyValidInputSucceeds(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-step2-json@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// Seed step 1 so step 2 can fully complete onboarding on success.
	start := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("last_period_start", start).Error; err != nil {
		t.Fatalf("seed last_period_start: %v", err)
	}

	jsonBody := `{"cycle_length":28,"period_length":5,"auto_period_fill":true,"irregular_cycle":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/2", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", authCookie)
	resp := mustAppResponse(t, app, req)
	defer func() { _ = resp.Body.Close() }()

	// A valid JSON body must not be rejected as "invalid input"; the JSON success
	// arm returns 200. The negated guard would surface a 400 validation error.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid JSON step2 status = %d, want 200 (body %q)", resp.StatusCode, mustReadBodyString(t, resp.Body))
	}

	var reloaded models.User
	if err := database.First(&reloaded, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.CycleLength != 28 || reloaded.PeriodLength != 5 {
		t.Fatalf("expected cycle/period settings persisted from JSON step2, got cycle=%d period=%d", reloaded.CycleLength, reloaded.PeriodLength)
	}
}

// TestOnboardingComplete_EligibleUserRedirectsToDashboard drives the standalone
// /api/v1/onboarding/complete happy path (handlers_onboarding_complete.go): an
// eligible owner (onboarding not yet completed, last_period_start already set)
// completes successfully and is redirected to /dashboard. This pins the
// `if err != nil` guard at line 27 — negating it to `== nil` turns a successful
// completion into an onboarding-finish error. Existing onboarding-complete tests
// reach this endpoint only AFTER step 2 has already completed onboarding, so they
// short-circuit at the eligibility check and never exercise this success arm.
func TestOnboardingComplete_EligibleUserRedirectsToDashboard(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-complete-success@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// Make the user eligible: onboarding still pending but a last period start on
	// record (step 1 already saved). This is the exact precondition under which
	// CompleteOnboardingForUser succeeds.
	start := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("last_period_start", start).Error; err != nil {
		t.Fatalf("seed last_period_start: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Cookie", authCookie)
	resp := mustAppResponse(t, app, req)
	defer func() { _ = resp.Body.Close() }()

	// HTMX success arm returns 200 with HX-Redirect /dashboard; the negated guard
	// would instead surface an onboarding-finish error status.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete status = %d, want 200 (body %q)", resp.StatusCode, mustReadBodyString(t, resp.Body))
	}
	if redirect := resp.Header.Get("HX-Redirect"); redirect != "/dashboard" {
		t.Fatalf("expected HX-Redirect /dashboard on successful completion, got %q", redirect)
	}

	var reloaded models.User
	if err := database.First(&reloaded, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !reloaded.OnboardingCompleted {
		t.Fatal("expected onboarding to be marked completed after success")
	}
}

// TestAuthTokenTTLConstants pins the two auth-token TTL constants declared in
// handler_types.go against their intended multiplied values. The ARITHMETIC_BASE
// mutants replace the '*' operators (7*24, 30*24, and *time.Hour) with '/', which
// collapses each Duration to ~0; asserting the exact multi-day durations kills all
// four. (The constants power remember-me vs default auth-token lifetimes consumed
// by handlers_auth_token_helpers.go.)
func TestAuthTokenTTLConstants(t *testing.T) {
	if defaultAuthTokenTTL != 7*24*time.Hour {
		t.Fatalf("defaultAuthTokenTTL = %v, want %v", defaultAuthTokenTTL, 7*24*time.Hour)
	}
	if defaultAuthTokenTTL != 168*time.Hour {
		t.Fatalf("defaultAuthTokenTTL = %v, want 168h", defaultAuthTokenTTL)
	}
	if rememberAuthTokenTTL != 30*24*time.Hour {
		t.Fatalf("rememberAuthTokenTTL = %v, want %v", rememberAuthTokenTTL, 30*24*time.Hour)
	}
	if rememberAuthTokenTTL != 720*time.Hour {
		t.Fatalf("rememberAuthTokenTTL = %v, want 720h", rememberAuthTokenTTL)
	}
	// Remember-me must outlast the default lifetime; a collapsed operator would
	// break this ordering too.
	if rememberAuthTokenTTL <= defaultAuthTokenTTL {
		t.Fatalf("remember-me TTL (%v) must exceed default TTL (%v)", rememberAuthTokenTTL, defaultAuthTokenTTL)
	}
}
