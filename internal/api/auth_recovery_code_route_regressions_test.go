package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestRecoveryCodePageRedirectsToDashboardWhenCookieMissing(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "recovery-route-missing-cookie@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("recovery-code request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", location)
	}
}

func TestRecoveryCodePageRejectsCookieFromDifferentUser(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	userB := createOnboardingTestUser(t, database, "recovery-cookie-user-b@example.com", "StrongPass1", true)
	authCookieUserB := loginAndExtractAuthCookie(t, app, userB.Email, "StrongPass1")
	_, recoveryCookieUserA := registerAndExtractRecoveryCookies(
		t,
		app,
		"recovery-cookie-user-a@example.com",
		"StrongPass1",
	)

	if recoveryCookieUserA == "" {
		t.Fatalf("expected recovery cookie for user A")
	}

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Cookie", authCookieUserB+"; "+recoveryCodeCookieName+"="+recoveryCookieUserA)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("recovery-code request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", location)
	}

	cleared := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if cleared == nil {
		t.Fatalf("expected invalid recovery cookie to be cleared")
	}
	if cleared.Value != "" {
		t.Fatalf("expected cleared recovery cookie value, got %q", cleared.Value)
	}
}

func TestRecoveryCodePageRejectsTamperedRecoveryCookie(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	authCookie, recoveryCookie := registerAndExtractRecoveryCookies(
		t,
		app,
		"recovery-cookie-tampered@example.com",
		"StrongPass1",
	)

	if authCookie == "" || recoveryCookie == "" {
		t.Fatalf("expected auth and recovery cookies in register response")
	}

	separatorIndex := strings.Index(recoveryCookie, ".")
	if separatorIndex < 0 || separatorIndex+6 >= len(recoveryCookie) {
		t.Fatalf("expected versioned recovery cookie payload, got %q", recoveryCookie)
	}

	tampered := recoveryCookie[:separatorIndex+5] + "A" + recoveryCookie[separatorIndex+6:]
	if recoveryCookie[separatorIndex+5] == 'A' {
		tampered = recoveryCookie[:separatorIndex+5] + "B" + recoveryCookie[separatorIndex+6:]
	}

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Cookie", authCookieName+"="+authCookie+"; "+recoveryCodeCookieName+"="+tampered)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("recovery-code request with tampered cookie failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/onboarding" {
		t.Fatalf("expected redirect to /onboarding, got %q", location)
	}

	cleared := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if cleared == nil {
		t.Fatalf("expected tampered recovery cookie to be cleared")
	}
	if cleared.Value != "" {
		t.Fatalf("expected cleared recovery cookie value, got %q", cleared.Value)
	}
}

func registerAndExtractRecoveryCookies(t *testing.T, app *fiber.App, email string, password string) (string, string) {
	t.Helper()

	form := url.Values{
		"email":            {email},
		"password":         {password},
		"confirm_password": {password},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	registerResponse, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer func() { _ = registerResponse.Body.Close() }()

	if registerResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected register status 303, got %d", registerResponse.StatusCode)
	}

	pickup := responseCookieValue(registerResponse.Cookies(), registerPickupCookieName)
	if pickup == "" {
		t.Fatalf("expected pickup cookie after register")
	}

	pickupRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	pickupRequest.Header.Set("Cookie", registerPickupCookieName+"="+pickup)
	pickupResponse, err := app.Test(pickupRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register/welcome request failed: %v", err)
	}
	defer func() { _ = pickupResponse.Body.Close() }()

	authCookie := responseCookieValue(pickupResponse.Cookies(), authCookieName)
	recoveryCookie := responseCookieValue(pickupResponse.Cookies(), recoveryCodeCookieName)
	return authCookie, recoveryCookie
}
