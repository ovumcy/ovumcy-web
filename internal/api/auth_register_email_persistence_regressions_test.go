package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRegisterPageKeepsEmailAfterPasswordValidationError(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	email := "persist-register@example.com"

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
		t.Fatalf("expected redirect location after register validation error")
	}
	parsedLocation, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if parsedLocation.Path != "/register" {
		t.Fatalf("expected redirect to /register, got %q", parsedLocation.Path)
	}
	if strings.TrimSpace(parsedLocation.RawQuery) != "" {
		t.Fatalf("expected redirect without query params, got %q", parsedLocation.RawQuery)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie in register redirect response")
	}

	registerRequest := httptest.NewRequest(http.MethodGet, "/register", nil)
	registerRequest.Header.Set("Accept-Language", "en")
	registerRequest.Header.Set("Cookie", flashCookieName+"="+flashValue)
	registerResponse, err := app.Test(registerRequest, -1)
	if err != nil {
		t.Fatalf("register page request failed: %v", err)
	}
	defer registerResponse.Body.Close()

	if registerResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", registerResponse.StatusCode)
	}

	body, err := io.ReadAll(registerResponse.Body)
	if err != nil {
		t.Fatalf("read register body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, `id="register-email" type="email" name="email" value="`+email+`"`) {
		t.Fatalf("expected register page to keep submitted email after validation error")
	}
	if !strings.Contains(rendered, "Use at least 8 characters with uppercase, lowercase, and a number.") {
		t.Fatalf("expected localized weak password message from flash")
	}

	afterFlashRequest := httptest.NewRequest(http.MethodGet, "/register", nil)
	afterFlashRequest.Header.Set("Accept-Language", "en")
	afterFlashResponse, err := app.Test(afterFlashRequest, -1)
	if err != nil {
		t.Fatalf("register request after flash consumption failed: %v", err)
	}
	defer afterFlashResponse.Body.Close()

	afterFlashBody, err := io.ReadAll(afterFlashResponse.Body)
	if err != nil {
		t.Fatalf("read body after flash consumption: %v", err)
	}
	clean := string(afterFlashBody)
	if strings.Contains(clean, `id="register-email" type="email" name="email" value="`+email+`"`) {
		t.Fatalf("did not expect register email to persist after flash is consumed")
	}
	if strings.Contains(clean, "Use at least 8 characters with uppercase, lowercase, and a number.") {
		t.Fatalf("did not expect weak-password error after flash is consumed")
	}
}
