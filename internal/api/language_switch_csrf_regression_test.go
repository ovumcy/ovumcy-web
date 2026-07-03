package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLanguageSwitchRequiresCSRFTokenWhenEnabled(t *testing.T) {
	app, _ := newOnboardingTestAppWithCSRF(t)

	request := httptest.NewRequest(http.MethodPost, "/lang", strings.NewReader(url.Values{
		"lang": {"ru"},
		"next": {"/login"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("language switch request without csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware status 403, got %d", response.StatusCode)
	}
}

func TestLanguageSwitchAcceptsValidCSRFTokenWhenEnabled(t *testing.T) {
	app, _ := newOnboardingTestAppWithCSRF(t)

	loginRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
	loginResponse, err := app.Test(loginRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login page request for csrf token failed: %v", err)
	}
	defer loginResponse.Body.Close()

	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected login page status 200, got %d", loginResponse.StatusCode)
	}

	csrfToken := extractCSRFTokenFromAuthPage(t, mustReadBodyString(t, loginResponse.Body))
	csrfCookie := responseCookie(loginResponse.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatal("expected csrf cookie in login response")
	}

	switchForm := url.Values{
		"csrf_token": {csrfToken},
		"lang":       {"ru"},
		"next":       {"/login"},
	}
	switchRequest := httptest.NewRequest(http.MethodPost, "/lang", strings.NewReader(switchForm.Encode()))
	switchRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	switchRequest.Header.Set("Cookie", cookiePair(csrfCookie))

	switchResponse, err := app.Test(switchRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("language switch request with csrf failed: %v", err)
	}
	defer switchResponse.Body.Close()

	if switchResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", switchResponse.StatusCode)
	}
	if location := switchResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}
	if value := responseCookieValue(switchResponse.Cookies(), languageCookieName); value != "ru" {
		t.Fatalf("expected ovumcy_lang cookie=ru, got %q", value)
	}
}
