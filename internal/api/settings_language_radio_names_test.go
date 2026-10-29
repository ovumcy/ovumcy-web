package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The visible chip is the bare code ("DE"), which a screen reader announces as
// "de". Each language radio carries a name that spells the language in its own
// script and still contains the visible code (WCAG 2.5.3 label in name).
func TestSettingsLanguageRadiosCarryANativeAccessibleName(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-language-names@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)
	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	for code, want := range map[string]string{"de": "Deutsch", "en": "English", "ru": "Русский"} {
		option := htmlFindElement(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "data-settings-interface-language-option") == code
		})
		if option == nil {
			t.Fatalf("expected the %s language option", code)
		}
		radio := htmlFindElement(option, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "input" && htmlAttr(node, "type") == "radio"
		})
		if radio == nil {
			t.Fatalf("%s: expected a radio input", code)
		}
		name := htmlAttr(radio, "aria-label")
		if !strings.Contains(name, want) || !strings.Contains(strings.ToLower(name), code) {
			t.Fatalf("%s: want an accessible name with %q and the visible code, got %q", code, want, name)
		}
	}
}
