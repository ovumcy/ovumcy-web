package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRegisterValidationErrorRedirectDoesNotLeakEmailOrErrorInQuery(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	email := "test@test.com"

	form := url.Values{
		"email":            {email},
		"password":         {"12345678"},
		"confirm_password": {"12345678"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	location := strings.TrimSpace(response.Header.Get("Location"))
	if location == "" {
		t.Fatalf("expected redirect location")
	}

	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if parsed.Path != "/register" {
		t.Fatalf("expected redirect path /register, got %q", parsed.Path)
	}
	if strings.TrimSpace(parsed.RawQuery) != "" {
		t.Fatalf("expected empty redirect query, got %q", parsed.RawQuery)
	}
	if strings.Contains(strings.ToLower(location), "test%40test.com") || strings.Contains(strings.ToLower(location), "test@test.com") {
		t.Fatalf("did not expect email leakage in redirect location: %q", location)
	}
	if strings.Contains(strings.ToLower(location), "weak+password") || strings.Contains(strings.ToLower(location), "error=") {
		t.Fatalf("did not expect error leakage in redirect location: %q", location)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie for register validation error")
	}
}
