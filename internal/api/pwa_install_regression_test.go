package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// htmlElementWithAttr finds the first element carrying an attribute, whatever its
// value: the install hooks are bare `data-*` markers, and htmlElementByAttr's
// empty-value form matches every element that lacks the attribute entirely.
func htmlElementWithAttr(root *html.Node, name string) *html.Node {
	return htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, name)
	})
}

func htmlElementsWithAttr(root *html.Node, name string) []*html.Node {
	return htmlFindElements(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, name)
	})
}

func TestBaseTemplateIncludesPWAMetadataAndInstallCopy(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	// Structural checks only: the manifest link and the theme-color meta hook must
	// be present. Exact href/sizes/content attribute strings and the "Install
	// Ovumcy" copy are incidental and would churn, so they are not pinned here.
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlElementByAttr(document, "rel", "manifest") == nil {
		t.Fatal(`expected a <link rel="manifest"> in the rendered page`)
	}
	if htmlElementByID(document, "theme-color-meta") == nil {
		t.Fatal("expected the theme-color meta element (id=theme-color-meta)")
	}
}

// TestBaseLayoutOffersInstallAsOneCompactRow pins the structural half of the
// re-homed install offer: the multi-line pitch (kicker + icon + heading + a
// per-mode paragraph + two stacked full-width buttons) ate half the first screen
// on a phone, so the layout now carries a single row — one install action plus a
// dismiss control — and nothing else.
func TestBaseLayoutOffersInstallAsOneCompactRow(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	if htmlElementWithAttr(document, "data-pwa-install-banner") != nil {
		t.Fatal("the expanded install banner is still rendered: the compact offer replaces it")
	}
	if copies := htmlElementsWithAttr(document, "data-pwa-install-copy"); len(copies) != 0 {
		t.Fatalf("expected the per-mode install paragraphs to move to settings, found %d in the layout", len(copies))
	}

	offer := htmlElementWithAttr(document, "data-pwa-install-offer")
	if offer == nil {
		t.Fatal("expected the compact install offer (data-pwa-install-offer) in the layout")
	}
	if got := htmlAttr(offer, "aria-live"); got != "polite" {
		t.Fatalf("expected the install offer to stay an aria-live=polite region, got aria-live=%q", got)
	}
	if !htmlHasAttr(offer, "hidden") {
		t.Fatal("expected the install offer to render hidden until the browser reports a pending prompt")
	}

	actions := htmlElementsWithAttr(offer, "data-pwa-install-action")
	if len(actions) != 2 {
		t.Fatalf("expected exactly two controls in the compact offer (install + dismiss), got %d", len(actions))
	}

	installAction := htmlElementByAttr(offer, "data-pwa-install-action", "install")
	if installAction == nil {
		t.Fatal("expected the compact offer to carry the install action")
	}
	if got := htmlAttr(installAction, "data-pwa-install-title-key"); got != "pwa.install.title" {
		t.Fatalf("expected the install action to declare its copy key, got %q", got)
	}

	dismissAction := htmlElementByAttr(offer, "data-pwa-install-action", "dismiss")
	if dismissAction == nil {
		t.Fatal("expected the compact offer to carry the dismiss action")
	}
	if strings.TrimSpace(htmlAttr(dismissAction, "aria-label")) == "" {
		t.Fatal("expected the glyph-only dismiss control to carry an accessible name")
	}
}

// TestSettingsInterfaceCarriesThePersistentInstallEntry pins the other half: the
// offer is dismissible on the first screen only because settings keeps a quiet,
// permanent entry point, with a hint for every install path the client can take.
func TestSettingsInterfaceCarriesThePersistentInstallEntry(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "pwa-install-settings@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	document := mustParseHTMLDocument(t, fetchPageBody(t, app, "/settings", authCookie))

	interfaceSection := htmlElementByID(document, "settings-interface")
	if interfaceSection == nil {
		t.Fatal("expected the settings interface section (id=settings-interface)")
	}

	row := htmlElementWithAttr(interfaceSection, "data-pwa-install-settings")
	if row == nil {
		t.Fatal("expected the install entry (data-pwa-install-settings) inside the interface section")
	}
	if htmlElementByAttr(row, "data-pwa-install-action", "install") == nil {
		t.Fatal("expected the settings install entry to carry the install action")
	}

	hints := make(map[string]bool)
	for _, hint := range htmlElementsWithAttr(row, "data-pwa-install-hint") {
		hints[htmlAttr(hint, "data-pwa-install-hint")] = true
	}
	for _, expected := range []string{"prompt", "ios", "menu", "installed"} {
		if !hints[expected] {
			t.Errorf("expected the settings install entry to carry the %q hint, got %v", expected, hints)
		}
	}
}
