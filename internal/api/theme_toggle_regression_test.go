package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBaseTemplateIncludesThemeToggleControl(t *testing.T) {
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

	if !strings.Contains(rendered, `class="theme-toggle"`) {
		t.Fatalf("expected rendered page to include theme toggle control")
	}
	if !strings.Contains(rendered, `data-theme-toggle`) {
		t.Fatalf("expected rendered page to include theme toggle mount point")
	}
	if !strings.Contains(rendered, ">Dark<") {
		t.Fatalf("expected rendered page to include default visible dark-theme label")
	}
}
