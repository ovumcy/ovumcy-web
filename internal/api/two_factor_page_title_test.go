package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestBrandedPageTitleFallsBackWhenTheCatalogueLacksAnEntry(t *testing.T) {
	if got := brandedPageTitle(map[string]string{}, "page.key", "Ovumcy | Fallback"); got != "Ovumcy | Fallback" {
		t.Errorf("expected the fallback for an untranslated page, got %q", got)
	}
	if got := brandedPageTitle(map[string]string{"page.key": "Page"}, "page.key", "x"); got != "Ovumcy | Page" {
		t.Errorf("expected the built-in app name when the catalogue has none, got %q", got)
	}
	if got := brandedPageTitle(map[string]string{"page.key": "Page", "app.name": "Овумси"}, "page.key", "x"); got != "Овумси | Page" {
		t.Errorf("expected the catalogue app name, got %q", got)
	}
}

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
