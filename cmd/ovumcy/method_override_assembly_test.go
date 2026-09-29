package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// The method override (WEB-107) is only correct at one place in the shipped
// chain: ahead of every limiter and CSRF, behind only app-wide Use middleware.
// Its own tests in internal/api mount it themselves, so these read the order
// off the production assembly.

// sendFormOverride POSTs a urlencoded form carrying _method (omitted when verb
// is empty), with the CSRF token in the form body when csrfCookie is set, the
// way a no-JS browser does.
func sendFormOverride(t *testing.T, app *fiber.App, csrfCookie *http.Cookie, target string, verb string) (int, []byte) {
	t.Helper()

	form := url.Values{"note": {"x"}}
	if verb != "" {
		form.Set("_method", verb)
	}
	if csrfCookie != nil {
		form.Set("csrf_token", csrfCookie.Value)
	}
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	if csrfCookie != nil {
		request.AddCookie(csrfCookie)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST %s with _method=%s: %v", target, verb, err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode, mustReadAll(t, response)
}

func TestMethodOverrideIsCountedByTheLimiterOfTheVerbItBecomes(t *testing.T) {
	handler, _ := newRateLimitTestHandlerAndDB(t)
	const target = "/api/v1/sessions/current"

	deleteKey := overflowRateLimitKey(t, handler, http.MethodDelete, target)
	postKey := overflowRateLimitKey(t, handler, http.MethodPost, target)
	if deleteKey == "" || deleteKey == postKey {
		t.Fatalf("precondition: a DELETE and a POST to %s must overflow different limiters, got %q and %q", target, deleteKey, postKey)
	}

	app := newScopeGuardApp(t, handler)
	csrfCookie := scopeGuardCSRFCookie(t, app)
	if status, body := sendFormOverride(t, app, csrfCookie, target, http.MethodDelete); status == http.StatusTooManyRequests {
		t.Fatalf("first override was refused: %s", body)
	}
	status, body := sendFormOverride(t, app, csrfCookie, target, http.MethodDelete)
	if status != http.StatusTooManyRequests {
		t.Fatalf("second override: status %d, want 429 from the DELETE-scoped limiter", status)
	}
	payload := struct {
		Error string `json:"error"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode 429: %v (body %q)", err, body)
	}
	if payload.Error != deleteKey {
		t.Fatalf("POST with _method=DELETE overflowed %q, want the DELETE limiter's %q: the override runs after the limiters", payload.Error, deleteKey)
	}
}

func TestMethodOverrideIsCheckedByTheProductionCSRFAsTheVerbItBecomes(t *testing.T) {
	handler, _ := newRateLimitTestHandlerAndDB(t)
	// A fresh app per request: the guard's budgets are 1, and a 429 would
	// answer ahead of CSRF.
	send := func(target string, withCSRF bool) int {
		app := newScopeGuardApp(t, handler)
		var csrfCookie *http.Cookie
		if withCSRF {
			csrfCookie = scopeGuardCSRFCookie(t, app)
		}
		status, _ := sendFormOverride(t, app, csrfCookie, target, http.MethodDelete)
		return status
	}

	if status := send("/api/v1/users/current/calendar-feed", true); status != http.StatusNoContent {
		t.Fatalf("with a CSRF pair: status %d, want the terminal 204", status)
	}
	if status := send("/api/v1/users/current/calendar-feed", false); status != http.StatusForbidden {
		t.Fatalf("without a token: status %d, want 403", status)
	}
	// The callback's POST exemption must not survive the override.
	if status := send(security.OIDCCallbackPath, false); status != http.StatusForbidden {
		t.Fatalf("OIDC callback POST with _method=DELETE: status %d, want 403", status)
	}
}

func TestShippedAppRoutesAFormOverrideToTheRouteOfThatVerb(t *testing.T) {
	handler, _ := newRateLimitTestHandlerAndDB(t)
	app := newFiberApp(runtimeConfig{Location: time.UTC, DefaultLanguage: "en", RateLimits: uniformRateLimits(t, 100, time.Minute)}, handler)
	csrfCookie := scopeGuardCSRFCookie(t, app)
	// Sign-out is registered only as DELETE, with its auth gate on the route
	// itself rather than on a group: a plain POST never reaches it (the
	// catch-all answers), and a POST the override turns into DELETE reaches the
	// route and its 401.
	const target = "/api/v1/sessions/current"

	if status, _ := sendFormOverride(t, app, csrfCookie, target, ""); status == http.StatusUnauthorized || status < http.StatusBadRequest {
		t.Fatalf("precondition: a plain form POST answered %d, want a refusal from outside the DELETE route", status)
	}
	status, body := sendFormOverride(t, app, csrfCookie, target, http.MethodDelete)
	if status != http.StatusUnauthorized {
		t.Fatalf("POST with _method=DELETE and no session: status %d (%s), want the route's 401", status, body)
	}
}

func TestFiberConfigLetsMiddlewareRunBeforeRouteMatching(t *testing.T) {
	handler, _ := newRateLimitTestHandlerAndDB(t)
	// With SkipUnmatchedRoutes a POST to a PUT/DELETE-only path is answered 405
	// before any middleware runs, and every no-JS override breaks silently.
	if fiberConfig(proxySettings{}, handler).SkipUnmatchedRoutes {
		t.Fatal("SkipUnmatchedRoutes must stay off: MethodOverride has to run before routing")
	}
}
