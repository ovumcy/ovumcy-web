package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAuthLogoutSupportsPostRequest(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "logout-get@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", strings.NewReader(url.Values{}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("logout POST request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}
}

func TestLogoutPageRoutePostRedirectsToLoginAndClearsAuthCookies(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "logout-page-route@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(url.Values{}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set(
		"Cookie",
		joinCookieHeader(
			authCookie,
			recoveryCodeCookieName+"=temporary-recovery",
			resetPasswordCookieName+"=temporary-reset",
		),
	)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("logout page route POST request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	authCookieAfterLogout := responseCookie(response.Cookies(), authCookieName)
	if authCookieAfterLogout == nil {
		t.Fatalf("expected logout response to clear auth cookie")
	}
	if authCookieAfterLogout.Value != "" {
		t.Fatalf("expected cleared auth cookie value, got %q", authCookieAfterLogout.Value)
	}

	recoveryCookieAfterLogout := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if recoveryCookieAfterLogout == nil {
		t.Fatalf("expected logout response to clear recovery code cookie")
	}
	if recoveryCookieAfterLogout.Value != "" {
		t.Fatalf("expected cleared recovery code cookie value, got %q", recoveryCookieAfterLogout.Value)
	}

	resetCookieAfterLogout := responseCookie(response.Cookies(), resetPasswordCookieName)
	if resetCookieAfterLogout == nil {
		t.Fatalf("expected logout response to clear reset password cookie")
	}
	if resetCookieAfterLogout.Value != "" {
		t.Fatalf("expected cleared reset password cookie value, got %q", resetCookieAfterLogout.Value)
	}
}
