package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// The per-IP logout row on DELETE /api/v1/sessions/current is edge middleware:
// it refuses before the handler, so a request it refuses keeps the caller's
// session alive. Every address behind one NAT shares it, and until it skipped
// failed requests any of them could spend it with unauthenticated DELETEs and
// hold every owner's API sign-out refused for the rest of the window. Refused
// requests (4xx) no longer count; a successful logout still does, which
// TestEveryRateLimiterAnswersThroughTheSharedEnvelope proves by spending the
// logout row with a real session.

// deleteCurrentSession sends DELETE /api/v1/sessions/current with the given
// Cookie header and, when csrfToken is set, the CSRF header that lets it past
// the CSRF middleware to the auth chain.
func deleteCurrentSession(t *testing.T, app *fiber.App, cookie string, csrfToken string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(""))
	request.Header.Set("Accept", "application/json")
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	if csrfToken != "" {
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	return response
}

// logOutWithSession signs an owner in on the production stack and ends that
// session through the API, returning the logout response.
func logOutWithSession(t *testing.T, app *fiber.App, database *gorm.DB, email string) *http.Response {
	t.Helper()

	authCookie := loginOnProductionStack(t, app, database, email)
	token, csrfCookie := issueCSRFFormCredentials(t, app)
	return deleteCurrentSession(t, app, authCookie+"; "+csrfCookie, token)
}

// TestLogoutEdgeBudgetIsNotSpentByRefusedRequests drives the shipped default
// per-IP logout budget with that many unauthenticated DELETEs — half without a
// CSRF token (refused by the CSRF middleware), half with one but no session
// (refused by the auth chain) — and then signs a real owner out from the same
// address. Before the fix the sixty refusals spent the row and the sign-out
// was answered 429 with the session still alive.
func TestLogoutEdgeBudgetIsNotSpentByRefusedRequests(t *testing.T) {
	minimalRuntimeEnv(t)
	limits := loadRateLimits(t)
	if limits.LogoutMax != 60 {
		t.Fatalf("shipped per-IP logout budget = %d, want 60; this test spends exactly that many", limits.LogoutMax)
	}

	handler, database := newRateLimitTestHandlerAndDB(t)
	app := newFiberApp(runtimeConfig{Location: time.UTC, DefaultLanguage: "en", RateLimits: limits}, handler)

	token, csrfCookie := issueCSRFFormCredentials(t, app)
	for attempt := 1; attempt <= limits.LogoutMax; attempt++ {
		cookie, header := "", ""
		if attempt%2 == 0 {
			cookie, header = csrfCookie, token
		}
		response := deleteCurrentSession(t, app, cookie, header)
		_ = response.Body.Close()
		if response.StatusCode < http.StatusBadRequest || response.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("unauthenticated logout %d answered %d, want a 4xx refusal other than 429", attempt, response.StatusCode)
		}
	}

	response := logOutWithSession(t, app, database, "logout-edge-budget@example.com")
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		t.Fatalf("a real sign-out after %d refused DELETEs from the same address was answered 429: the per-IP logout row counted requests it refused", limits.LogoutMax)
	}
	if response.StatusCode >= http.StatusBadRequest {
		t.Fatalf("a real sign-out answered %d, want success: %s", response.StatusCode, mustReadAll(t, response))
	}
}

// TestLogoutEdgeBudgetIsNotSpentByCaseVariantRefusals spends the per-IP logout
// row with a spelling the case-insensitive router still routes to the logout
// endpoint and the row still matches — /API/v1/sessions/current — sent with a
// valid CSRF token, no session and no Accept header. The auth chain has to
// refuse it as the API request it is (a 4xx), not redirect it to the sign-in
// page: a 303 is below 400, so the row counts it despite skipping failed
// requests, and sixty of them held a real owner's sign-out at 429.
func TestLogoutEdgeBudgetIsNotSpentByCaseVariantRefusals(t *testing.T) {
	minimalRuntimeEnv(t)
	limits := loadRateLimits(t)
	if limits.LogoutMax != 60 {
		t.Fatalf("shipped per-IP logout budget = %d, want 60; this test spends exactly that many", limits.LogoutMax)
	}

	handler, database := newRateLimitTestHandlerAndDB(t)
	app := newFiberApp(runtimeConfig{Location: time.UTC, DefaultLanguage: "en", RateLimits: limits}, handler)

	token, csrfCookie := issueCSRFFormCredentials(t, app)
	for attempt := 1; attempt <= limits.LogoutMax; attempt++ {
		request := httptest.NewRequest(http.MethodDelete, "/API/v1/sessions/current", strings.NewReader(""))
		request.Header.Set("Cookie", csrfCookie)
		request.Header.Set("X-CSRF-Token", token)
		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("case-variant logout request failed: %v", err)
		}
		_ = response.Body.Close()
		if response.StatusCode < http.StatusBadRequest || response.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("sessionless DELETE /API/v1/sessions/current %d answered %d, want a 4xx refusal other than 429", attempt, response.StatusCode)
		}
	}

	response := logOutWithSession(t, app, database, "logout-edge-case-variant@example.com")
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		t.Fatalf("a real sign-out after %d case-variant sessionless DELETEs from the same address was answered 429: the per-IP logout row counted requests the auth chain refused", limits.LogoutMax)
	}
	if response.StatusCode >= http.StatusBadRequest {
		t.Fatalf("a real sign-out answered %d, want success: %s", response.StatusCode, mustReadAll(t, response))
	}
}
