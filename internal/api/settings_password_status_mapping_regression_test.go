package api

import (
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
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", form, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

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

	request := httptest.NewRequest(http.MethodPut, "/api/v1/users/current/password", strings.NewReader(`{"current_password":`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("change-password request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", form, map[string]string{
		"HX-Request":      "true",
		"Accept-Language": "en",
	})
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for htmx inline error, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	flash := htmlFlashByKey(document, "settings.error.invalid_current_password")
	if flash == nil {
		t.Fatal("expected htmx response to carry invalid_current_password flash key")
	}
	if !htmlHasClass(flash, "status-error") {
		t.Fatal("expected htmx flash wrapper to use the shared status-error class")
	}
	if got := htmlAttr(flash, "data-flash-status"); got != "error" {
		t.Fatalf("expected htmx flash status %q, got %q", "error", got)
	}
}
