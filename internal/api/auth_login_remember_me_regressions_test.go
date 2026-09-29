package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
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
	defer func() { _ = sessionResponse.Body.Close() }()

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
	defer func() { _ = rememberResponse.Body.Close() }()

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

// TestLoginRememberMeIsReadFromTheBodyOnly pins the flag's single source. fiber's
// FormValue searched the URL query before the body, so `?remember_me=1` on a
// sign-in whose body said false (or said nothing) minted the 30-day cookie: a
// crafted link chose the session's lifetime for whoever submitted the form.
func TestLoginRememberMeIsReadFromTheBodyOnly(t *testing.T) {
	loginCookie := func(t *testing.T, contentType, body, query string, wantStatus int) *http.Cookie {
		t.Helper()
		app, database := newOnboardingTestApp(t)
		user := createOnboardingTestUser(t, database, "remember-body@example.com", "StrongPass1", true)
		body = strings.ReplaceAll(body, "{email}", url.QueryEscape(user.Email))
		if strings.Contains(contentType, "json") {
			body = strings.ReplaceAll(body, url.QueryEscape(user.Email), user.Email)
		}

		request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions"+query, strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != wantStatus {
			t.Fatalf("status = %d, want %d", response.StatusCode, wantStatus)
		}
		cookie := responseCookie(response.Cookies(), authCookieName)
		if cookie == nil {
			t.Fatal("expected an auth cookie")
		}
		return cookie
	}
	const formBase = "email={email}&password=StrongPass1"

	sessionScoped := []struct {
		name, contentType, body, query string
		wantStatus                     int
	}{
		{"json false with ?remember_me=1", "application/json", `{"email":"{email}","password":"StrongPass1","remember_me":false}`, "?remember_me=1", http.StatusOK},
		{"json without the flag with ?remember_me=true", "application/json", `{"email":"{email}","password":"StrongPass1"}`, "?remember_me=true", http.StatusOK},
		{"form without the flag with ?remember_me=1", "application/x-www-form-urlencoded", formBase, "?remember_me=1", http.StatusSeeOther},
		{"form remember_me=0 with ?remember_me=1", "application/x-www-form-urlencoded", formBase + "&remember_me=0", "?remember_me=1", http.StatusSeeOther},
	}
	for _, tc := range sessionScoped {
		t.Run(tc.name, func(t *testing.T) {
			cookie := loginCookie(t, tc.contentType, tc.body, tc.query, tc.wantStatus)
			if !cookie.Expires.IsZero() || cookie.MaxAge != 0 {
				t.Fatalf("the query flag made the cookie persistent (Expires=%s, MaxAge=%d)", cookie.Expires, cookie.MaxAge)
			}
		})
	}

	// Positive controls: the same transports still honour the flag when the body
	// carries it, so the refusals above cannot be a login that never persists.
	t.Run("form remember_me=1 in the body is persistent", func(t *testing.T) {
		cookie := loginCookie(t, "application/x-www-form-urlencoded", formBase+"&remember_me=1", "", http.StatusSeeOther)
		if cookie.Expires.IsZero() {
			t.Fatal("expected a persistent cookie for a body remember_me=1")
		}
	})
	t.Run("json remember_me true in the body is persistent", func(t *testing.T) {
		cookie := loginCookie(t, "application/json", `{"email":"{email}","password":"StrongPass1","remember_me":true}`, "", http.StatusOK)
		if cookie.Expires.IsZero() {
			t.Fatal("expected a persistent cookie for a body remember_me=true")
		}
	})
}

// TestLoginWithASecondFactorKeepsTheBodyRememberMeThroughTheChallenge covers the
// two-step path: the flag is sealed into the pending cookie at the password
// step and decides the session the challenge mints, so a query flag that
// reached the first step would surface only after the second.
func TestLoginWithASecondFactorKeepsTheBodyRememberMeThroughTheChallenge(t *testing.T) {
	for _, tc := range []struct {
		name           string
		loginBody      string
		wantPersistent bool
	}{
		{"body false with ?remember_me=1 stays session scoped", `{"email":"{email}","password":"StrongPass1","remember_me":false}`, false},
		{"body true is persistent (control)", `{"email":"{email}","password":"StrongPass1","remember_me":true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, database := newOnboardingTestAppWithCSRF(t)
			user := createOnboardingTestUser(t, database, "remember-totp@example.com", "StrongPass1", true)
			rawSecret := setupTOTPForUser(t, database, user.ID, []byte("test-secret-key"))
			csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)

			loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions?remember_me=1",
				strings.NewReader(strings.ReplaceAll(tc.loginBody, "{email}", user.Email)))
			loginRequest.Header.Set("Content-Type", "application/json")
			loginRequest.Header.Set("X-CSRF-Token", csrfToken)
			loginRequest.Header.Set("Cookie", csrfCookieHeader)
			loginResponse, err := app.Test(loginRequest, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("login request failed: %v", err)
			}
			defer func() { _ = loginResponse.Body.Close() }()
			pending := responseCookie(loginResponse.Cookies(), totpPendingCookieName)
			if pending == nil || pending.Value == "" {
				t.Fatalf("expected a pending second-factor cookie, status %d", loginResponse.StatusCode)
			}

			code, err := totp.GenerateCode(rawSecret, time.Now())
			if err != nil {
				t.Fatalf("GenerateCode: %v", err)
			}
			challengeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge",
				strings.NewReader(`{"code":"`+code+`"}`))
			challengeRequest.Header.Set("Content-Type", "application/json")
			challengeRequest.Header.Set("X-CSRF-Token", csrfToken)
			challengeRequest.Header.Set("Cookie", joinCookieHeader(cookiePair(pending), csrfCookieHeader))
			challengeResponse, err := app.Test(challengeRequest, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("challenge request failed: %v", err)
			}
			defer func() { _ = challengeResponse.Body.Close() }()
			authCookie := responseCookie(challengeResponse.Cookies(), authCookieName)
			if authCookie == nil || authCookie.Value == "" {
				t.Fatalf("expected an auth cookie after the challenge, status %d", challengeResponse.StatusCode)
			}
			if persistent := !authCookie.Expires.IsZero(); persistent != tc.wantPersistent {
				t.Fatalf("persistent = %t, want %t", persistent, tc.wantPersistent)
			}
		})
	}
}
