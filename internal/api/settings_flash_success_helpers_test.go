package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func assertSettingsFlashSuccessScenario(t *testing.T, path string, form url.Values, successLabel string) {
	t.Helper()

	ctx := newSettingsSecurityTestContext(t, "settings-user@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, path, form, nil)
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect %q, got %q", "/settings", location)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie for settings success message")
	}

	followRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	followRequest.Header.Set("Accept-Language", "en")
	followRequest.Header.Set("Cookie", ctx.authCookie+"; "+flashCookieName+"="+flashValue)

	followResponse, err := ctx.app.Test(followRequest, -1)
	if err != nil {
		t.Fatalf("follow-up settings request failed: %v", err)
	}
	defer followResponse.Body.Close()

	if followResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected follow-up status 200, got %d", followResponse.StatusCode)
	}

	body, err := io.ReadAll(followResponse.Body)
	if err != nil {
		t.Fatalf("read follow-up body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, successLabel) {
		t.Fatalf("expected success label %q in settings page", successLabel)
	}
	if strings.Contains(rendered, weakPasswordErrorText) {
		t.Fatalf("did not expect weak password error on success page")
	}

	afterFlashRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	afterFlashRequest.Header.Set("Accept-Language", "en")
	afterFlashRequest.Header.Set("Cookie", ctx.authCookie)

	afterFlashResponse, err := ctx.app.Test(afterFlashRequest, -1)
	if err != nil {
		t.Fatalf("settings request after flash consumption failed: %v", err)
	}
	defer afterFlashResponse.Body.Close()

	afterFlashBody, err := io.ReadAll(afterFlashResponse.Body)
	if err != nil {
		t.Fatalf("read settings body after flash consumption: %v", err)
	}
	if strings.Contains(string(afterFlashBody), successLabel) {
		t.Fatalf("did not expect success label after flash is consumed")
	}
}
