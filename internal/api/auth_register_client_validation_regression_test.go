package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterPageIncludesClientValidationHooks(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/register", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read register body: %v", err)
	}
	rendered := string(body)

	expectedFragments := []string{
		`id="register-form"`,
		`data-password-mismatch-message="Passwords do not match."`,
		`id="register-client-status"`,
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected register page to include %q", fragment)
		}
	}
}
