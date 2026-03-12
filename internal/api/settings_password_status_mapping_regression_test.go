package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSettingsChangePasswordInvalidCurrentPasswordJSONStatus(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-password-json-status@example.com")

	form := url.Values{
		"current_password": {"WrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/change-password", form, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid current password" {
		t.Fatalf("expected invalid current password error, got %q", got)
	}
}

func TestSettingsChangePasswordInvalidInputJSONStatus(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-password-invalid-input@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/settings/change-password", strings.NewReader(`{"current_password":`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("change-password request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid settings input" {
		t.Fatalf("expected invalid settings input error, got %q", got)
	}
}

func TestSettingsChangePasswordInvalidCurrentPasswordHTMXInlineError(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-password-htmx-inline@example.com")

	form := url.Values{
		"current_password": {"WrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/change-password", form, map[string]string{
		"HX-Request":      "true",
		"Accept-Language": "en",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for htmx inline error, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read htmx response body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, `class="status-error"`) {
		t.Fatalf("expected status-error markup in htmx response, got %q", rendered)
	}
	if !strings.Contains(rendered, "Current password is incorrect.") {
		t.Fatalf("expected localized invalid current password message, got %q", rendered)
	}
}
