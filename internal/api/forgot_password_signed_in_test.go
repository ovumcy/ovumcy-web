package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A signed-in owner has no use for the reset-request form (the password change
// lives in settings), so the page sends them where /login does instead of
// rendering a form that goes nowhere.
func TestForgotPasswordPageRedirectsASignedInOwner(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forgot-signed-in@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/forgot-password", nil)
	request.Header.Set("Cookie", authCookie)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("signed-in forgot-password request failed: %v", err)
	}
	_ = response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 for a signed-in owner, got %d", response.StatusCode)
	}
	if got := response.Header.Get("Location"); got != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", got)
	}
}

func TestForgotPasswordPageStillRendersForAGuest(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/forgot-password", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("guest forgot-password request failed: %v", err)
	}
	_ = response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for a guest, got %d", response.StatusCode)
	}
}
