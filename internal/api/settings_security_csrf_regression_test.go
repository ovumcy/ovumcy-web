package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSettingsChangePasswordMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-password-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodPost, "/api/settings/change-password", url.Values{
		"current_password": {"StrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}, nil)
	defer response.Body.Close()

	assertStatusCode(t, response, http.StatusForbidden)
}

func TestSettingsRegenerateRecoveryCodeMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-regenerate-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodPost, "/api/settings/regenerate-recovery-code", url.Values{}, nil)
	defer response.Body.Close()

	assertStatusCode(t, response, http.StatusForbidden)
}

func TestSettingsClearDataMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-clear-data-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodPost, "/api/settings/clear-data", url.Values{
		"password": {"StrongPass1"},
	}, nil)
	defer response.Body.Close()

	assertStatusCode(t, response, http.StatusForbidden)
}

func TestSettingsDeleteAccountMissingCSRFRejectedByMiddleware(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-delete-account-csrf@example.com")

	response := settingsRequestWithoutCSRF(t, ctx, http.MethodDelete, "/api/settings/delete-account", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

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

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request without csrf %s %s failed: %v", method, path, err)
	}
	return response
}
