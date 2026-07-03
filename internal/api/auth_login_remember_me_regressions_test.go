package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLoginRememberMeControlsCookiePersistence(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "remember-session@example.com", "StrongPass1", true)

	sessionForm := url.Values{
		"email":    {user.Email},
		"password": {"StrongPass1"},
	}
	sessionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(sessionForm.Encode()))
	sessionRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	sessionResponse, err := app.Test(sessionRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("session login request failed: %v", err)
	}
	defer sessionResponse.Body.Close()

	if sessionResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", sessionResponse.StatusCode)
	}

	sessionCookie := responseCookie(sessionResponse.Cookies(), authCookieName)
	if sessionCookie == nil {
		t.Fatalf("expected auth cookie for default session login")
	}
	if !sessionCookie.Expires.IsZero() {
		t.Fatalf("expected session cookie without Expires when remember_me is disabled")
	}

	rememberForm := url.Values{
		"email":       {user.Email},
		"password":    {"StrongPass1"},
		"remember_me": {"1"},
	}
	rememberRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(rememberForm.Encode()))
	rememberRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rememberResponse, err := app.Test(rememberRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("remember-me login request failed: %v", err)
	}
	defer rememberResponse.Body.Close()

	if rememberResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", rememberResponse.StatusCode)
	}

	rememberCookie := responseCookie(rememberResponse.Cookies(), authCookieName)
	if rememberCookie == nil {
		t.Fatalf("expected auth cookie for remember-me login")
	}
	if rememberCookie.Expires.IsZero() {
		t.Fatalf("expected persistent auth cookie when remember_me is enabled")
	}
	if rememberCookie.Expires.Before(time.Now().Add(20 * 24 * time.Hour)) {
		t.Fatalf("expected remember-me cookie to expire in ~30 days, got %s", rememberCookie.Expires)
	}
}
