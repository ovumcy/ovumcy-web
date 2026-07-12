package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/net/html"
)

func TestNotFoundPageForGuestUsesLoginPrimaryAction(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/missing-page", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("not-found page request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	document := mustParseHTMLDocument(t, rendered)
	title := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-not-found-title")
	})
	if title == nil {
		t.Fatalf("expected not-found title element with stable hook")
	}
	if got := htmlAttr(title, "data-title-key"); got != "not_found.title" {
		t.Fatalf("expected not-found title key %q, got %q", "not_found.title", got)
	}
	// Assert the primary-action element itself points to /login, addressed by
	// its stable data hook — not a whole-page substring scan, which nav/footer
	// links to /login would satisfy regardless (that is why the currentUser
	// guard mutant survived).
	guestPrimaryAction := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-not-found-primary-action")
	})
	if guestPrimaryAction == nil {
		t.Fatalf("expected not-found primary action element with stable hook")
	}
	if got := htmlAttr(guestPrimaryAction, "href"); got != "/login" {
		t.Fatalf("expected guest not-found primary action href %q, got %q", "/login", got)
	}
	// Privacy navigation belongs in the footer, not as an inline 404 action.
	// Assert semantically (no /privacy anchor inside the not-found section)
	// rather than pinning a class string that any styling change would break.
	page := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-not-found-page")
	})
	if page == nil {
		t.Fatalf("expected not-found page section with stable hook")
	}
	inlinePrivacy := htmlFindElements(page, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && htmlAttr(node, "href") == "/privacy"
	})
	if len(inlinePrivacy) != 0 {
		t.Fatalf("did not expect an inline privacy action in the not-found page; the footer already provides privacy navigation")
	}
}

func TestNotFoundPageForAuthenticatedUserUsesDashboardPrimaryAction(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "not-found-owner@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/missing-owner-page", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("authenticated not-found page request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	document := mustParseHTMLDocument(t, rendered)
	primaryAction := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-not-found-primary-action")
	})
	if primaryAction == nil {
		t.Fatalf("expected not-found primary action element with stable hook")
	}
	if got := htmlAttr(primaryAction, "href"); got != "/dashboard" {
		t.Fatalf("expected authenticated not-found primary action href %q, got %q", "/dashboard", got)
	}
	if strings.Contains(rendered, "not-found-owner") {
		t.Fatalf("did not expect authenticated identity in not-found page layout")
	}
}

func TestNotFoundAPIPathReturnsJSONError(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/api/missing-endpoint", nil)
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("not-found api request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}

	errorMessage := readAPIError(t, response.Body)
	if errorMessage != "not found" {
		t.Fatalf("expected not found api error, got %q", errorMessage)
	}
}

func TestNotFoundHTMXPathReturnsLocalizedStatusErrorMarkup(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/missing-fragment", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "ru")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("not-found htmx request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	document := mustParseHTMLDocument(t, rendered)
	flash := htmlFlashByKey(document, "not_found.title")
	if flash == nil {
		t.Fatalf("expected htmx not-found response to carry not_found.title flash key, got %q", rendered)
	}
	if !htmlHasClass(flash, "status-error") {
		t.Fatalf("expected htmx not-found wrapper to use status-error class")
	}
	if normalizeHTMLText(htmlNodeText(flash)) == "" {
		t.Fatalf("expected localized not-found htmx message body, got empty")
	}
	if strings.Contains(rendered, "<html") {
		t.Fatalf("expected htmx branch to avoid full page markup, got %q", rendered)
	}
}

// TestNotFoundHTMXNeverLeaksRawTitleKeyWhenTranslationMissing pins the fallback
// branch in respondNotFoundMappedError (error_mapping_pages.go L61): when the
// "not_found.title" translation is MISSING, translateMessage echoes the raw i18n
// key, and the `message == "not_found.title"` guard must replace it with a human
// fallback so the user never sees the bare key. A CONDITIONALS_NEGATION mutant
// (`==` -> `!=`) inverts the guard, letting the raw key render as the visible
// HTMX message. Drive the responder directly with an empty message catalog so
// the fallback branch is the one under test, and assert the visible flash text
// is never the raw key (a no-raw-key-leak invariant, not a copy assertion).
func TestNotFoundHTMXNeverLeaksRawTitleKeyWhenTranslationMissing(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/probe", func(c fiber.Ctx) error {
		// Empty catalog => translateMessage returns the key verbatim, forcing the
		// human-fallback branch.
		c.Locals(contextMessagesKey, map[string]string{})
		return respondNotFoundMappedError(c)
	})

	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	request.Header.Set("HX-Request", "true")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("not-found htmx probe failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	flash := htmlFlashByKey(document, "not_found.title")
	if flash == nil {
		t.Fatal("expected htmx not-found response to carry the not_found.title flash key")
	}
	if got := normalizeHTMLText(htmlNodeText(flash)); got == "not_found.title" {
		t.Fatalf("htmx not-found message leaked the raw i18n key as visible text: %q", got)
	}
}
