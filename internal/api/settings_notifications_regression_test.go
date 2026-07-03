package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSettingsFlashErrorTakesPrecedenceOverQueryError(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-notify-error@example.com")

	form := url.Values{
		"current_password": {"WrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", form, nil)
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie for settings error")
	}

	followRequest := httptest.NewRequest(http.MethodGet, "/settings?error=invalid%20profile%20input", nil)
	followRequest.Header.Set("Accept-Language", "en")
	followRequest.Header.Set("Cookie", ctx.authCookie+"; "+flashCookieName+"="+flashValue)
	followResponse, err := ctx.app.Test(followRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer followResponse.Body.Close()

	document := mustParseHTMLDocument(t, mustReadBodyString(t, followResponse.Body))
	if htmlFlashByKey(document, "settings.error.invalid_current_password") == nil {
		t.Fatal("expected flash error keyed to invalid_current_password in settings page")
	}
	if htmlFlashByKey(document, "settings.error.invalid_profile_input") != nil {
		t.Fatal("expected flash error to take precedence over query error")
	}
}

func TestSettingsStatusIgnoresQueryWhenFlashMissing(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-notify-status@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings?status=password_changed", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer response.Body.Close()

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlFlashByKey(document, "settings.success.password_changed") != nil {
		t.Fatal("expected query status to be ignored without flash state")
	}
}

func TestSettingsFlashSuccessTakesPrecedenceOverQueryStatus(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-notify-success@example.com")

	form := url.Values{"display_name": {"Maya"}}
	form.Set("csrf_token", ctx.csrfToken)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/profile", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("profile update request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie for settings success")
	}

	followRequest := httptest.NewRequest(http.MethodGet, "/settings?status=password_changed", nil)
	followRequest.Header.Set("Accept-Language", "en")
	followRequest.Header.Set("Cookie", ctx.authCookie+"; "+flashCookieName+"="+flashValue)
	followResponse, err := ctx.app.Test(followRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer followResponse.Body.Close()

	document := mustParseHTMLDocument(t, mustReadBodyString(t, followResponse.Body))
	if htmlFlashByKey(document, "settings.success.profile_updated") == nil {
		t.Fatal("expected flash success keyed to settings.success.profile_updated")
	}
	if htmlFlashByKey(document, "settings.success.password_changed") != nil {
		t.Fatal("expected flash success to take precedence over query status")
	}
}
