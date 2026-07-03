package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"golang.org/x/net/html"
)

func TestPrivacyRouteRendersPublicPage(t *testing.T) {
	app := newTestAppWithPrivacyRoute(t)

	request := httptest.NewRequest(http.MethodGet, "/privacy", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	expectedSections := []string{
		"zero-collection",
		"your-data",
		"your-rights",
		"retention",
		"hidden-sections",
		"predictions",
		"third-parties",
		"open-source",
	}
	gotSections := privacySectionIDs(document)
	for _, expected := range expectedSections {
		if !slices.Contains(gotSections, expected) {
			t.Fatalf("expected privacy section %q, got %v", expected, gotSections)
		}
	}

	if !privacyHasBackLink(document, "/login") {
		t.Fatal("expected privacy back link to point to /login for guest users")
	}
}

func TestPrivacyRouteBackLinkForAuthenticatedUser(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "privacy-auth@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/privacy", nil)
	request.Header.Set("Cookie", authCookie)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if !privacyHasBackLink(document, "/dashboard") {
		t.Fatal("expected privacy back link to point to /dashboard for authenticated users")
	}
	if !privacyHasBreadcrumbLink(document, "/dashboard") {
		t.Fatal("expected privacy breadcrumb to link back to /dashboard for authenticated users")
	}
}

func privacySectionIDs(root *html.Node) []string {
	sections := htmlFindElements(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "section" && htmlHasAttr(node, "data-privacy-section")
	})
	ids := make([]string, 0, len(sections))
	for _, section := range sections {
		ids = append(ids, htmlAttr(section, "data-privacy-section"))
	}
	return ids
}

func privacyHasBackLink(root *html.Node, target string) bool {
	return htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && htmlAttr(node, "href") == target
	}) != nil
}

func privacyHasBreadcrumbLink(root *html.Node, target string) bool {
	breadcrumb := htmlFindElement(root, func(node *html.Node) bool {
		if node.Type != html.ElementNode || node.Data != "p" {
			return false
		}
		if !htmlHasClass(node, "journal-muted") || !htmlHasClass(node, "text-sm") {
			return false
		}
		return htmlFindElement(node, func(child *html.Node) bool {
			return child.Type == html.ElementNode && child.Data == "a" && htmlAttr(child, "href") == target
		}) != nil
	})
	return breadcrumb != nil
}
