package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestSettingsFlashErrorTakesPrecedenceOverQueryError(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-notify-error@example.com")

	form := url.Values{
		"current_password": {"WrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", form, nil)
	defer func() { _ = response.Body.Close() }()

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
	defer func() { _ = followResponse.Body.Close() }()

	document := mustParseHTMLDocument(t, mustReadBodyString(t, followResponse.Body))
	if htmlFlashByKey(document, "settings.error.invalid_current_password") == nil {
		t.Fatal("expected flash error keyed to invalid_current_password in settings page")
	}
	if htmlFlashByKey(document, "settings.error.invalid_profile_input") != nil {
		t.Fatal("expected flash error to take precedence over query error")
	}
}

// settingsSuccessFlashCookie performs a real settings mutation (a profile rename)
// and returns the sealed flash cookie it sets, so a test can render the settings
// page in a state where a success flash genuinely exists.
func settingsSuccessFlashCookie(t *testing.T, ctx settingsSecurityTestContext) string {
	t.Helper()

	form := url.Values{"display_name": {"Maya"}}
	form.Set("csrf_token", ctx.csrfToken)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/profile", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("profile update request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie for settings success")
	}
	return flashValue
}

// settingsPageDocument renders /settings with the given query string and cookie
// header and returns the parsed document, asserting the page actually rendered
// (a redirect to /login would otherwise satisfy any "element absent" assertion).
func settingsPageDocument(t *testing.T, ctx settingsSecurityTestContext, query string, cookieHeader string) *html.Node {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/settings"+query, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", cookieHeader)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected the settings page to render with 200, got %d", response.StatusCode)
	}
	return mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
}

// TestSettingsStatusIgnoresQueryWhenFlashMissing pins that ?status= is never a
// flash source: without flash state the page shows no success banner.
//
// The claim is a NEGATIVE one, so it needs a positive anchor in the same test or
// it is satisfied by a settings page that renders no flash at all. Verified that
// this is not hypothetical: breaking the success-flash `data-flash-key` hook in
// settings.html leaves the assertion below green while the sibling precedence
// test fails. The anchor renders the same URL through the same helper with a real
// flash cookie first, so a page or hook that can no longer surface a flash fails
// here rather than passing quietly.
func TestSettingsStatusIgnoresQueryWhenFlashMissing(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-notify-status@example.com")

	anchored := settingsPageDocument(t, ctx, "?status=password_changed",
		joinCookieHeader(ctx.authCookie, flashCookieName+"="+settingsSuccessFlashCookie(t, ctx)))
	if htmlFlashByKey(anchored, "settings.success.profile_updated") == nil {
		t.Fatal("anchor failed: the settings page does not surface a success flash that genuinely exists, so the negative assertion below would prove nothing")
	}

	document := settingsPageDocument(t, ctx, "?status=password_changed", ctx.authCookie)
	if htmlFlashByKey(document, "settings.success.password_changed") != nil {
		t.Fatal("expected query status to be ignored without flash state")
	}
}

func TestSettingsFlashSuccessTakesPrecedenceOverQueryStatus(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-notify-success@example.com")

	flashValue := settingsSuccessFlashCookie(t, ctx)

	document := settingsPageDocument(t, ctx, "?status=password_changed",
		joinCookieHeader(ctx.authCookie, flashCookieName+"="+flashValue))
	if htmlFlashByKey(document, "settings.success.profile_updated") == nil {
		t.Fatal("expected flash success keyed to settings.success.profile_updated")
	}
	if htmlFlashByKey(document, "settings.success.password_changed") != nil {
		t.Fatal("expected flash success to take precedence over query status")
	}
}
