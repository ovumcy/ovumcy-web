package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// The timezone cookie is the second of the two plaintext preference caches that
// are neither sealed nor session-scoped. Like `ovumcy_lang` it is retracted when
// the owner ends the session on purpose and left alone when the server merely
// refuses one — but unlike the language it is re-issued by LanguageMiddleware
// from a request header, so the retraction has an ordering to prove: a logout
// whose own request carries X-Ovumcy-Timezone must still end with the cookie
// retracted, not re-set by the middleware that ran before the handler.

// TestLogoutRetractsTheTimezoneCookie pins both shapes of the logout request:
// with and without the timezone header the signed-in client attaches to every
// htmx call. The header case is the one that would regress silently — the
// middleware re-issues the cookie on the way in, and only the handler's later
// retraction makes the response honest.
func TestLogoutRetractsTheTimezoneCookie(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{name: "no timezone header"},
		{name: "request carries the timezone header", header: "Europe/Belgrade"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			app, authCookie, csrfCookie, csrfToken := prepareAuthenticatedLogoutCSRFContext(t)

			form := url.Values{"csrf_token": {csrfToken}}
			request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if testCase.header != "" {
				request.Header.Set(timezoneHeaderName, testCase.header)
			}
			request.Header.Set(
				"Cookie",
				joinCookieHeader(
					authCookie,
					cookiePair(csrfCookie),
					timezoneCookieName+"=America/Toronto",
				),
			)

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("logout request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusSeeOther {
				t.Fatalf("expected status 303, got %d", response.StatusCode)
			}

			cleared := responseCookie(response.Cookies(), timezoneCookieName)
			if cleared == nil {
				t.Fatalf("expected logout to retract %s", timezoneCookieName)
			}
			if strings.TrimSpace(cleared.Value) != "" {
				t.Fatalf("expected %s retracted with an empty value, got %q", timezoneCookieName, cleared.Value)
			}
			if !cleared.Expires.Before(time.Now()) {
				t.Fatalf("expected %s retracted with a past expiry, got %s", timezoneCookieName, cleared.Expires)
			}
		})
	}
}

// TestSessionRejectionLeavesTheTimezoneCookieInPlace is the boundary, mirroring
// the language cookie's. A rejection is not an ending the owner chose: it fires
// on an expired cookie mid-use and on any unauthenticated probe carrying a stale
// one. Both rejection shapes are covered, the unsupported role included — that
// is the branch running clearAuthRelatedCookies, which would silently inherit
// the retraction if it were added to that helper instead of to
// clearSessionEndCookies. The auth cookie's own retraction is the anchor, so a
// response that cleared nothing at all fails here rather than passing.
func TestSessionRejectionLeavesTheTimezoneCookieInPlace(t *testing.T) {
	tests := []struct {
		name       string
		authCookie func(t *testing.T, database *gorm.DB) string
	}{
		{
			name: "invalid token",
			authCookie: func(t *testing.T, _ *gorm.DB) string {
				t.Helper()
				return authCookieName + "=not-a-sealed-value"
			},
		},
		{
			name: "unsupported role",
			authCookie: func(t *testing.T, database *gorm.DB) string {
				t.Helper()
				user := createOnboardingTestUser(t, database, "tz-reject-role@example.com", "StrongPass1", true)
				if err := database.Model(&user).Update("role", "partner").Error; err != nil {
					t.Fatalf("set unsupported legacy role: %v", err)
				}
				user.Role = "partner"
				return issueAuthCookieForUser(t, user)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)

			request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
			request.Header.Set("Accept-Language", "en")
			request.Header.Set("Cookie", joinCookieHeader(testCase.authCookie(t, database), timezoneCookieName+"=America/Toronto"))

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusSeeOther)

			clearedAuth := responseCookie(response.Cookies(), authCookieName)
			if clearedAuth == nil || strings.TrimSpace(clearedAuth.Value) != "" {
				t.Fatalf("expected the rejected session to clear the auth cookie, got %#v", clearedAuth)
			}
			if cookie := responseCookie(response.Cookies(), timezoneCookieName); cookie != nil {
				t.Fatalf("expected the rejected session to leave %s alone, got %#v", timezoneCookieName, cookie)
			}
		})
	}
}
