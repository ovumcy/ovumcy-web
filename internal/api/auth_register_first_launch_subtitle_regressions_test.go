package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/html"
)

func TestRegisterPageShowsFirstLaunchSubtitleWhenNoUsers(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/register", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register page request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "data-subtitle-key") == "auth.register_subtitle"
	}) == nil {
		t.Fatalf("expected first launch subtitle with data-subtitle-key hook")
	}
}

func TestRegisterPageHidesFirstLaunchSubtitleWhenUserExists(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	createOnboardingTestUser(t, database, "owner@example.com", "StrongPass1", true)

	request := httptest.NewRequest(http.MethodGet, "/register", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register page request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "data-subtitle-key") == "auth.register_subtitle"
	}) != nil {
		t.Fatalf("expected first launch subtitle to be hidden when users already exist")
	}
}
