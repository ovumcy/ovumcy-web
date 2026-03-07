package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBaseTemplateIncludesPWAMetadataAndInstallCopy(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	rendered := string(body)

	expectedFragments := []string{
		`<link rel="manifest" href="/static/manifest.webmanifest">`,
		`<link rel="apple-touch-icon" sizes="180x180" href="/static/pwa/apple-touch-icon.png">`,
		`<meta id="theme-color-meta" name="theme-color" content="#fff9f0">`,
		`<meta name="apple-mobile-web-app-capable" content="yes">`,
		`Install Ovumcy`,
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected rendered page to include %q", fragment)
		}
	}
}
