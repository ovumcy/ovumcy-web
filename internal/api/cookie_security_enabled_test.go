package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSecureFlashCookieEnabledWhenConfigured(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithCookieSecure(t, true)
	user := createOnboardingTestUser(t, database, "secure-cookies@example.com", "StrongPass1", true)

	response := mustAppResponse(t, app, secureCookieFormRequest(http.MethodPost, "/api/auth/login", url.Values{
		"email":    {user.Email},
		"password": {"WrongPass1"},
	}))
	assertStatusCode(t, response, http.StatusSeeOther)

	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil {
		t.Fatal("expected flash cookie on invalid login")
	}
	if !flashCookie.Secure {
		t.Fatal("expected flash cookie Secure=true when COOKIE_SECURE is enabled")
	}
}

func TestSecureLanguageCookieEnabledWhenConfigured(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithCookieSecure(t, true)
	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/lang/en?next=/login", nil))
	assertStatusCode(t, response, http.StatusSeeOther)

	languageCookie := responseCookie(response.Cookies(), languageCookieName)
	if languageCookie == nil {
		t.Fatal("expected language cookie on language switch")
	}
	if !languageCookie.Secure {
		t.Fatal("expected language cookie Secure=true when COOKIE_SECURE is enabled")
	}
}

func TestSecureAuthCookieEnabledWhenConfigured(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithCookieSecure(t, true)
	user := createOnboardingTestUser(t, database, "secure-auth-cookie@example.com", "StrongPass1", true)

	response := mustAppResponse(t, app, secureCookieFormRequest(http.MethodPost, "/api/auth/login", url.Values{
		"email":    {user.Email},
		"password": {"StrongPass1"},
	}))
	assertStatusCode(t, response, http.StatusSeeOther)

	authCookie := responseCookie(response.Cookies(), authCookieName)
	if authCookie == nil {
		t.Fatal("expected auth cookie on valid login")
	}
	if !authCookie.HttpOnly {
		t.Fatal("expected auth cookie HttpOnly=true")
	}
	if !authCookie.Secure {
		t.Fatal("expected auth cookie Secure=true when COOKIE_SECURE is enabled")
	}
	if authCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected auth cookie SameSite=Lax, got %v", authCookie.SameSite)
	}
}

func TestSecureRecoveryCookieEnabledWhenConfigured(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithCookieSecure(t, true)
	response := mustAppResponse(t, app, secureCookieFormRequest(http.MethodPost, "/api/auth/register", url.Values{
		"email":            {"recovery-cookie-secure@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
	}))
	assertStatusCode(t, response, http.StatusSeeOther)

	recoveryCookie := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if recoveryCookie == nil {
		t.Fatal("expected recovery cookie after successful register")
	}
	if !recoveryCookie.HttpOnly {
		t.Fatal("expected recovery cookie HttpOnly=true")
	}
	if !recoveryCookie.Secure {
		t.Fatal("expected recovery cookie Secure=true when COOKIE_SECURE is enabled")
	}
	if recoveryCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected recovery cookie SameSite=Lax, got %v", recoveryCookie.SameSite)
	}
}

func secureCookieFormRequest(method string, target string, form url.Values) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}
