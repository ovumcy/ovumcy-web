package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Every page title reads "Ovumcy | <page>"; the 2FA settings page took the bare
// catalogue entry instead, in every language but the English fallback.
func TestTwoFactorSettingsPageTitleCarriesTheAppName(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "two-factor-title@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for _, lang := range []string{"en", "ru"} {
		request := httptest.NewRequest(http.MethodGet, "/settings/2fa", nil)
		request.Header.Set("Accept-Language", lang)
		request.Header.Set("Cookie", authCookie)
		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, http.StatusOK)
		document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

		title := htmlFindElement(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "title"
		})
		if title == nil {
			t.Fatalf("%s: expected a <title>", lang)
		}
		if text := strings.TrimSpace(htmlNodeText(title)); !strings.HasPrefix(text, "Ovumcy | ") {
			t.Errorf("%s: expected the title to start with the app name, got %q", lang, text)
		}
	}
}
