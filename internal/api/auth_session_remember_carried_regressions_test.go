package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Remembering a device is the owner's choice, and a session re-issue is not an
// occasion to re-take it. refreshCurrentSession runs after every change to the
// account's security posture — password, 2FA on or off, clear-data — and used to
// pass rememberMe=false unconditionally, quietly demoting a 30-day cookie to a
// session one. That is the same defect as remembering a device nobody chose,
// pointing the other way, so it belongs to the same class and is pinned here.
//
// The choice is read back off the live token's own lifetime rather than from a
// new claim: the session already records it, and a second source of truth is a
// second thing to get wrong.
func TestChangingThePasswordKeepsTheRememberedDevice(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		remember       bool
		wantRemembered bool
	}{
		{name: "remembered session stays remembered", remember: true, wantRemembered: true},
		{name: "session-scoped stays session-scoped", remember: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			app, database := newOnboardingTestAppWithCSRF(t)
			user := createOnboardingTestUser(t, database, "remember-carried-"+strings.ReplaceAll(testCase.name, " ", "-")+"@example.com", "StrongPass1", true)

			authCookie, initial := loginWithRememberChoice(t, app, user.Email, "StrongPass1", testCase.remember)
			if remembered := !initial.Expires.IsZero(); remembered != testCase.wantRemembered {
				t.Fatalf("login itself did not honour the choice: remembered=%t, want %t", remembered, testCase.wantRemembered)
			}

			csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)
			form := url.Values{
				"current_password": {"StrongPass1"},
				"new_password":     {"EvenStronger2"},
				"confirm_password": {"EvenStronger2"},
				"csrf_token":       {csrfToken},
			}
			request := httptest.NewRequest(http.MethodPut, "/api/v1/users/current/password", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Cookie", settingsCookieHeader(authCookie, csrfCookie))
			response := mustAppResponse(t, app, request)

			reissued := responseCookie(response.Cookies(), authCookieName)
			if reissued == nil || strings.TrimSpace(reissued.Value) == "" {
				t.Fatalf("expected the password change to re-issue the session, got %#v", reissued)
			}
			remembered := !reissued.Expires.IsZero()
			if remembered != testCase.wantRemembered {
				t.Fatalf("expected the re-issued cookie remembered=%t, got %t (expires %v)", testCase.wantRemembered, remembered, reissued.Expires)
			}
			if remembered {
				if remaining := time.Until(reissued.Expires); remaining < 29*24*time.Hour {
					t.Fatalf("expected the re-issue to carry the remembered lifetime, got %s", remaining)
				}
			}
		})
	}
}

// loginWithRememberChoice signs in through the real form so the remembered-ness
// under test is the one the login path actually mints, not one sealed by hand.
func loginWithRememberChoice(t *testing.T, app *fiber.App, email string, password string, remember bool) (string, *http.Cookie) {
	t.Helper()

	page := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/login", nil))
	csrfToken := extractCSRFTokenFromAuthPage(t, mustReadBodyString(t, page.Body))
	csrfCookie := responseCookie(page.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatal("expected a csrf cookie on the login page")
	}

	form := url.Values{"email": {email}, "password": {password}, "csrf_token": {csrfToken}}
	if remember {
		form.Set("remember_me", "1")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", cookiePair(csrfCookie))
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	authCookie := responseCookie(response.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected an auth cookie on the login response")
	}
	return authCookieName + "=" + authCookie.Value, authCookie
}
