package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSecureCookiesDisabledByDefault(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "insecure-cookies@example.com", "StrongPass1", true)

	loginForm := url.Values{
		"email":    {user.Email},
		"password": {"StrongPass1"},
	}
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(loginForm.Encode()))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	loginResponse, err := app.Test(loginRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer loginResponse.Body.Close()

	authCookie := responseCookie(loginResponse.Cookies(), authCookieName)
	assertSessionCookieInsecure(t, authCookie, "auth")

	registerForm := url.Values{
		"email":            {"recovery-cookie-default@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
		"consent":          {"true"},
	}
	registerRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(registerForm.Encode()))
	registerRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	registerResponse, err := app.Test(registerRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer registerResponse.Body.Close()

	if registerResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected register status 303, got %d", registerResponse.StatusCode)
	}

	pickupCookie := responseCookie(registerResponse.Cookies(), registerPickupCookieName)
	assertSessionCookieInsecure(t, pickupCookie, "pickup")

	pickupRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	pickupRequest.Header.Set("Cookie", registerPickupCookieName+"="+pickupCookie.Value)
	pickupResponse, err := app.Test(pickupRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("pickup request failed: %v", err)
	}
	defer pickupResponse.Body.Close()

	recoveryCookie := responseCookie(pickupResponse.Cookies(), recoveryCodeCookieName)
	assertSessionCookieInsecure(t, recoveryCookie, "recovery")

	languageForm := url.Values{
		"lang": {"en"},
		"next": {"/login"},
	}
	languageRequest := httptest.NewRequest(http.MethodPost, "/lang", strings.NewReader(languageForm.Encode()))
	languageRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	languageResponse, err := app.Test(languageRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("language switch request failed: %v", err)
	}
	defer languageResponse.Body.Close()

	languageCookie := responseCookie(languageResponse.Cookies(), languageCookieName)
	if languageCookie == nil {
		t.Fatal("expected language cookie on language switch")
	}
	if languageCookie.Secure {
		t.Fatal("expected language cookie Secure=false when COOKIE_SECURE is disabled")
	}
}

func assertSessionCookieInsecure(t *testing.T, cookie *http.Cookie, label string) {
	t.Helper()
	if cookie == nil {
		t.Fatalf("expected %s cookie", label)
	}
	if !cookie.HttpOnly {
		t.Fatalf("expected %s cookie HttpOnly=true", label)
	}
	if cookie.Secure {
		t.Fatalf("expected %s cookie Secure=false when COOKIE_SECURE is disabled", label)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected %s cookie SameSite=Lax, got %v", label, cookie.SameSite)
	}
}
