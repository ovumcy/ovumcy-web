package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestSettingsChangePasswordMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-password-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", url.Values{
		"current_password": {"StrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}, nil)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusForbidden)
}

func TestSettingsInterfaceMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/interface", url.Values{
		"language": {"en"},
		"theme":    {"dark"},
	}, nil)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusForbidden)
}

func TestSettingsRegenerateRecoveryCodeMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-regenerate-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{}, nil)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusForbidden)
}

func TestSettingsTimezoneMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-timezone-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/timezone", url.Values{
		"timezone": {"Europe/Belgrade"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusForbidden)

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user after csrf-rejected timezone post: %v", err)
	}
	if reloaded.Timezone != "" {
		t.Fatalf("expected no timezone persisted when CSRF rejects the request, got %q", reloaded.Timezone)
	}
}

func TestSettingsClearDataMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-clear-data-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{
		"password": {"StrongPass1"},
	}, nil)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusForbidden)
}

func TestSettingsClearDataValidateMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-clear-data-validate-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe/validate", url.Values{
		"password": {"StrongPass1"},
	}, nil)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusForbidden)
}

func TestSettingsDeleteAccountMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-delete-account-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusForbidden)
}

func settingsRequestWithoutCSRF(t *testing.T, ctx settingsSecurityTestContext, method string, path string, form url.Values, headers map[string]string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request without csrf %s %s failed: %v", method, path, err)
	}
	return response
}
