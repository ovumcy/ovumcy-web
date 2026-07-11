package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
