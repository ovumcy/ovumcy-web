package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBaseTemplateIncludesThemeBootstrapAndToggleHooks(t *testing.T) {
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
		`window.localStorage.getItem("ovumcy_theme")`,
		`data-theme-toggle`,
		`data-theme-label-dark="Switch to dark mode"`,
		`data-theme-label-light="Switch to light mode"`,
		`data-theme-name-dark="Dark"`,
		`data-theme-name-light="Light"`,
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected rendered page to include %q", fragment)
		}
	}
}
