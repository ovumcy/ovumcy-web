package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSettingsChangePasswordReissuesSessionAndRejectsPreviousCookie(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "change-password-session@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", url.Values{
		"current_password": {"StrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	newAuthCookie := responseCookie(response.Cookies(), authCookieName)
	if newAuthCookie == nil || newAuthCookie.Value == "" {
		t.Fatal("expected password change response to issue a fresh auth cookie")
	}

	oldSessionRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	oldSessionRequest.Header.Set("Accept-Language", "en")
	oldSessionRequest.Header.Set("Cookie", ctx.authCookie)

	oldSessionResponse, err := ctx.app.Test(oldSessionRequest, -1)
	if err != nil {
		t.Fatalf("old session settings request failed: %v", err)
	}
	defer oldSessionResponse.Body.Close()

	if oldSessionResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected previous auth cookie to be rejected with 303, got %d", oldSessionResponse.StatusCode)
	}
	if location := oldSessionResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected previous auth cookie redirect to /login, got %q", location)
	}

	freshSessionRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	freshSessionRequest.Header.Set("Accept-Language", "en")
	freshSessionRequest.Header.Set("Cookie", cookiePair(newAuthCookie))

	freshSessionResponse, err := ctx.app.Test(freshSessionRequest, -1)
	if err != nil {
		t.Fatalf("fresh session settings request failed: %v", err)
	}
	defer freshSessionResponse.Body.Close()

	if freshSessionResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected fresh auth cookie to stay valid, got %d", freshSessionResponse.StatusCode)
	}
}

// TestSettingsChangePasswordHTMXRespondsWithSuccessStatusMarkup pins the
// HTMX branch of respondPasswordChanged: the success response is the
// shared dismissible status markup (localized, with the English fallback
// policy living in htmxSettingsSuccessMarkup).
func TestSettingsChangePasswordHTMXRespondsWithSuccessStatusMarkup(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "change-password-htmx@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", url.Values{
		"current_password": {"StrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}, map[string]string{
		"HX-Request": "true",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	body := mustReadBodyString(t, response.Body)
	if !strings.Contains(body, "status-ok") {
		t.Fatalf("expected dismissible status-ok markup in HTMX response, got %q", body)
	}
}
