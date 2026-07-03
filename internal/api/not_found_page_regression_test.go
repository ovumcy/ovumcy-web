package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	defer response.Body.Close()

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
	if !strings.Contains(rendered, `href="/login"`) {
		t.Fatalf("expected login primary action for guest not-found page")
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
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read authenticated not-found page body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, `href="/dashboard"`) {
		t.Fatalf("expected dashboard primary action for authenticated not-found page")
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
	defer response.Body.Close()

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
	defer response.Body.Close()

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
