package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSettingsChangePasswordInvalidCurrentPasswordShowsTopErrorBanner(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-password-error@example.com")

	form := url.Values{
		"current_password": {"WrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", form, map[string]string{
		"Accept-Language": "en",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", location)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie for invalid current password")
	}

	followRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	followRequest.Header.Set("Accept-Language", "en")
	followRequest.Header.Set("Cookie", ctx.authCookie+"; "+flashCookieName+"="+flashValue)
	followResponse, err := ctx.app.Test(followRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("follow-up settings request failed: %v", err)
	}
	defer followResponse.Body.Close()

	rendered := mustReadBodyString(t, followResponse.Body)
	document := mustParseHTMLDocument(t, rendered)
	flash := htmlFlashByKey(document, "settings.error.invalid_current_password")
	if flash == nil {
		t.Fatal("expected flash error keyed to settings.error.invalid_current_password")
	}
	if got := htmlAttr(flash, "data-flash-target"); got != "change_password" {
		t.Fatalf("expected change-password flash target attribute, got %q", got)
	}

	flashTag := `data-flash-key="settings.error.invalid_current_password"`
	accountAnchor := `id="settings-account"`
	flashIdx := strings.Index(rendered, flashTag)
	accountIdx := strings.Index(rendered, accountAnchor)
	if flashIdx < 0 || accountIdx < 0 || flashIdx >= accountIdx {
		t.Fatalf("expected change-password flash to render in top banner area before #settings-account (flash=%d, account=%d)", flashIdx, accountIdx)
	}
}
