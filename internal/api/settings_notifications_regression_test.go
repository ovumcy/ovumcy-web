package api

import (
	"io"
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
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/change-password", form, nil)
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
	followResponse, err := ctx.app.Test(followRequest, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer followResponse.Body.Close()

	body, err := io.ReadAll(followResponse.Body)
	if err != nil {
		t.Fatalf("read settings body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, "Current password is incorrect.") {
		t.Fatalf("expected flash error text in settings page")
	}
	if strings.Contains(rendered, "Unable to process profile data.") {
		t.Fatalf("expected flash error to take precedence over query error")
	}
}

func TestSettingsStatusIgnoresQueryWhenFlashMissing(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-notify-status@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings?status=password_changed", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read settings body: %v", err)
	}
	if strings.Contains(string(body), "Password changed successfully.") {
		t.Fatalf("expected query status to be ignored without flash state")
	}
}

func TestSettingsFlashSuccessTakesPrecedenceOverQueryStatus(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-notify-success@example.com")

	form := url.Values{"display_name": {"Maya"}}
	form.Set("csrf_token", ctx.csrfToken)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/profile", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))

	response, err := ctx.app.Test(request, -1)
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
	followResponse, err := ctx.app.Test(followRequest, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer followResponse.Body.Close()

	body, err := io.ReadAll(followResponse.Body)
	if err != nil {
		t.Fatalf("read settings body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, "Profile updated successfully.") {
		t.Fatalf("expected flash success message")
	}
	if strings.Contains(rendered, "Password changed successfully.") {
		t.Fatalf("expected flash success to take precedence over query status")
	}
}
