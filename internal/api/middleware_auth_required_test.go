package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAuthRequiredRefusesACaseVariantAPIPathAsAnAPIRequest pins every refusal
// branch of AuthRequired to the routing-normalized path. The router is
// case-insensitive, so /API/v1/days reaches the same API handler as
// /api/v1/days; with no Accept header the gate must still answer it as an API
// request — a JSON 4xx — and never with the browser redirect, which is below
// 400 and so counted by a limiter that skips only failed requests.
func TestAuthRequiredRefusesACaseVariantAPIPathAsAnAPIRequest(t *testing.T) {
	app, database := newOnboardingTestApp(t)

	onboardingUser := createOnboardingTestUser(t, database, "auth-required-onboarding@example.com", "StrongPass1", false)
	unsupportedUser := createOnboardingTestUser(t, database, "auth-required-role@example.com", "StrongPass1", true)
	if err := database.Model(&unsupportedUser).Update("role", "partner").Error; err != nil {
		t.Fatalf("set unsupported legacy role: %v", err)
	}
	unsupportedUser.Role = "partner"

	cases := []struct {
		name   string
		cookie string
	}{
		{name: "no session", cookie: ""},
		{name: "unsupported role", cookie: issueAuthCookieForUser(t, unsupportedUser)},
		{name: "onboarding required", cookie: issueAuthCookieForUser(t, onboardingUser)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/API/v1/days", nil)
			if testCase.cookie != "" {
				request.Header.Set("Cookie", testCase.cookie)
			}
			response := mustAppResponse(t, app, request)
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode < http.StatusBadRequest {
				t.Fatalf("GET /API/v1/days answered %d (Location %q), want a JSON 4xx: AuthRequired read the raw path and took the browser branch", response.StatusCode, response.Header.Get("Location"))
			}
			if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/json") {
				t.Fatalf("GET /API/v1/days answered Content-Type %q, want application/json", contentType)
			}
		})
	}
}

// TestAuthRequiredLetsEveryRoutableSpellingOfSignOutPastTheOnboardingGate: an
// owner who has not finished onboarding may still sign out, and the router
// sends /API/v1/sessions/current and /api/v1/sessions/current/ to the same
// sign-out handler. The onboarding exemption must therefore hold for those
// spellings too, not refuse them as onboarding-required. The lowercase row is
// the positive control.
func TestAuthRequiredLetsEveryRoutableSpellingOfSignOutPastTheOnboardingGate(t *testing.T) {
	app, database := newOnboardingTestApp(t)

	for index, path := range []string{
		"/api/v1/sessions/current",
		"/API/v1/sessions/current",
		"/api/v1/sessions/current/",
	} {
		// A fresh owner per spelling: signing out revokes the session the
		// cookie carries.
		user := createOnboardingTestUser(t, database, fmt.Sprintf("sign-out-onboarding-%d@example.com", index), "StrongPass1", false)
		request := httptest.NewRequest(http.MethodDelete, path, nil)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", issueAuthCookieForUser(t, user))

		response := mustAppResponse(t, app, request)
		body := mustReadBodyString(t, response.Body)
		if response.StatusCode >= http.StatusBadRequest {
			t.Errorf("DELETE %s by an owner mid-onboarding answered %d %q, want the sign-out to go through like the lowercase spelling", path, response.StatusCode, body)
		}
	}
}
