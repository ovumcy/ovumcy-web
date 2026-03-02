package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func loginAndExtractAuthCookie(t *testing.T, app *fiber.App, email string, password string) string {
	t.Helper()

	form := url.Values{
		"email":    {email},
		"password": {password},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected login status 303, got %d", response.StatusCode)
	}

	for _, cookie := range response.Cookies() {
		if cookie.Name == "ovumcy_auth" && cookie.Value != "" {
			return cookie.Name + "=" + cookie.Value
		}
	}

	t.Fatal("auth cookie is missing in login response")
	return ""
}

func loginAndExtractAuthCookieWithCSRF(t *testing.T, app *fiber.App, email string, password string) string {
	t.Helper()

	csrfRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
	csrfResponse, err := app.Test(csrfRequest, -1)
	if err != nil {
		t.Fatalf("load login page for csrf token failed: %v", err)
	}
	defer csrfResponse.Body.Close()

	if csrfResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected login page status 200, got %d", csrfResponse.StatusCode)
	}

	body, err := io.ReadAll(csrfResponse.Body)
	if err != nil {
		t.Fatalf("read login page body for csrf token failed: %v", err)
	}
	csrfToken := extractCSRFTokenFromAuthPage(t, string(body))
	csrfCookie := responseCookie(csrfResponse.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatal("csrf cookie is missing in login page response")
	}

	form := url.Values{
		"email":      {email},
		"password":   {password},
		"csrf_token": {csrfToken},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", csrfCookie.Name+"="+csrfCookie.Value)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("login request with csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected login status 303, got %d", response.StatusCode)
	}

	for _, cookie := range response.Cookies() {
		if cookie.Name == authCookieName && cookie.Value != "" {
			return cookie.Name + "=" + cookie.Value
		}
	}

	t.Fatal("auth cookie is missing in login response")
	return ""
}

var csrfTokenMetaPatternForAuthTests = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

func extractCSRFTokenFromAuthPage(t *testing.T, html string) string {
	t.Helper()

	match := csrfTokenMetaPatternForAuthTests.FindStringSubmatch(html)
	if len(match) < 2 {
		t.Fatalf("expected csrf token meta tag in auth page html")
	}

	token := strings.TrimSpace(match[1])
	if token == "" {
		t.Fatalf("expected non-empty csrf token value")
	}
	return token
}
