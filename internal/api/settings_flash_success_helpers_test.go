package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/net/html"
)

func assertSettingsFlashSuccessScenario(t *testing.T, method string, path string, form url.Values, successKey string) {
	t.Helper()

	ctx := newSettingsSecurityTestContext(t, "settings-user@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, method, path, form, nil)
	defer func() { _ = response.Body.Close() }()

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
	authCookieHeader := ctx.authCookie
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && authCookie.Value != "" {
		authCookieHeader = cookiePair(authCookie)
	}

	followRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	followRequest.Header.Set("Accept-Language", "en")
	followRequest.Header.Set("Cookie", joinCookieHeader(authCookieHeader, flashCookieName+"="+flashValue))

	followResponse, err := ctx.app.Test(followRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("follow-up settings request failed: %v", err)
	}
	defer func() { _ = followResponse.Body.Close() }()

	if followResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected follow-up status 200, got %d", followResponse.StatusCode)
	}

	followBody := mustReadBodyString(t, followResponse.Body)
	followDocument := mustParseHTMLDocument(t, followBody)
	if htmlFlashByKey(followDocument, successKey) == nil {
		t.Fatalf("expected flash success key %q in settings page", successKey)
	}
	if htmlFindElement(followDocument, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "data-flash-status") == "error"
	}) != nil {
		t.Fatalf("did not expect error flash on settings success page")
	}

	afterFlashRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	afterFlashRequest.Header.Set("Accept-Language", "en")
	afterFlashRequest.Header.Set("Cookie", authCookieHeader)

	afterFlashResponse, err := ctx.app.Test(afterFlashRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request after flash consumption failed: %v", err)
	}
	defer func() { _ = afterFlashResponse.Body.Close() }()

	afterFlashDocument := mustParseHTMLDocument(t, mustReadBodyString(t, afterFlashResponse.Body))
	if htmlFlashByKey(afterFlashDocument, successKey) != nil {
		t.Fatalf("did not expect flash success key %q after flash is consumed", successKey)
	}
}
