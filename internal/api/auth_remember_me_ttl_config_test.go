package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestSetRememberMeTTLDaysRefusesOutOfRange(t *testing.T) {
	handler := &Handler{}
	for _, days := range []int{-1, 0, RememberMeTTLDaysMin - 1, RememberMeTTLDaysMax + 1} {
		if err := handler.SetRememberMeTTLDays(days); err == nil {
			t.Errorf("SetRememberMeTTLDays(%d) accepted a value outside [%d, %d]", days, RememberMeTTLDaysMin, RememberMeTTLDaysMax)
		}
		if got := handler.rememberMeLifetime(); got != rememberAuthTokenTTL {
			t.Errorf("after refusing %d days the lifetime is %s, want the default %s", days, got, rememberAuthTokenTTL)
		}
	}
	for _, days := range []int{RememberMeTTLDaysMin, RememberMeTTLDaysDefault, RememberMeTTLDaysMax} {
		if err := handler.SetRememberMeTTLDays(days); err != nil {
			t.Errorf("SetRememberMeTTLDays(%d): %v", days, err)
		}
		if got, want := handler.rememberMeLifetime(), time.Duration(days)*24*time.Hour; got != want {
			t.Errorf("SetRememberMeTTLDays(%d) left the lifetime at %s, want %s", days, got, want)
		}
	}
}

// sessionWasRemembered tells a remembered session from an ordinary one by a
// lifetime longer than defaultAuthTokenTTL; a floor at or below it would demote
// a remembered device to a session cookie on its next posture re-issue.
func TestRememberMeTTLFloorStaysAboveTheDefaultLifetime(t *testing.T) {
	if floor := time.Duration(RememberMeTTLDaysMin) * 24 * time.Hour; floor <= defaultAuthTokenTTL {
		t.Fatalf("RememberMeTTLDaysMin = %s, must exceed defaultAuthTokenTTL %s", floor, defaultAuthTokenTTL)
	}
}

// Lowering REMEMBER_ME_TTL_DAYS has to end the longer sessions issued before it,
// not leave them running to their own sealed expiry: a token sealed with a
// lifetime above the one the instance issues now is refused on its next request.
func TestSessionLongerThanTheConfiguredLifetimeIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name          string
		configured    int
		tokenLifetime time.Duration
		wantAccepted  bool
	}{
		{name: "default instance accepts its own 30-day token", tokenLifetime: rememberAuthTokenTTL, wantAccepted: true},
		{name: "default instance refuses a 31-day token", tokenLifetime: 31 * 24 * time.Hour},
		{name: "instance lowered to 8 days refuses a 30-day token", configured: RememberMeTTLDaysMin, tokenLifetime: rememberAuthTokenTTL},
		{name: "instance lowered to 8 days still accepts a 7-day session", configured: RememberMeTTLDaysMin, tokenLifetime: defaultAuthTokenTTL, wantAccepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{rememberMeTTLDays: tc.configured})
			user := createOnboardingTestUser(t, database, "remember-ttl-cap@example.com", "StrongPass1", true)
			token, _, err := services.NewAuthService(nil).BuildAuthSessionTokenWithSessionID([]byte(testAppSecretKey), user.ID, user.Role, user.AuthSessionVersion, tc.tokenLifetime, time.Now())
			if err != nil {
				t.Fatalf("build auth session token: %v", err)
			}

			request := httptest.NewRequest(http.MethodGet, "/settings", nil)
			request.Header.Set("Cookie", authCookieName+"="+sealAuthCookieTokenForTest(t, token))
			response := mustAppResponse(t, app, request)

			if accepted := response.StatusCode == http.StatusOK; accepted != tc.wantAccepted {
				t.Fatalf("GET /settings with a %s token: status %d, want accepted=%t", tc.tokenLifetime, response.StatusCode, tc.wantAccepted)
			}
			if !tc.wantAccepted {
				cleared := responseCookie(response.Cookies(), authCookieName)
				if cleared == nil || cleared.Value != "" {
					t.Fatalf("a refused session must retract its cookie, got %#v", cleared)
				}
			}
		})
	}
}

// The checkbox label is where the owner consents to a lifetime, so it states the
// one the instance grants rather than a fixed 30 days.
func TestLoginRememberMeLabelStatesTheConfiguredLifetime(t *testing.T) {
	for _, tc := range []struct {
		configured int
		want       string
	}{
		{configured: 0, want: "Stay signed in for 30 days"},
		{configured: RememberMeTTLDaysMax, want: "Stay signed in for 365 days"},
	} {
		app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{rememberMeTTLDays: tc.configured})
		body := mustReadBodyString(t, mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/login", nil)).Body)
		if !strings.Contains(body, tc.want) {
			t.Errorf("configured=%d: login page does not say %q", tc.configured, tc.want)
		}
	}
}

// The session service's default ceiling and the default remember-me lifetime
// are one number: a drift would refuse every default remembered session, or
// admit longer ones.
func TestDefaultSessionCeilingMatchesTheDefaultRememberMeLifetime(t *testing.T) {
	if rememberAuthTokenTTL != services.DefaultMaxAuthSessionLifetime {
		t.Fatalf("rememberAuthTokenTTL %s != services.DefaultMaxAuthSessionLifetime %s", rememberAuthTokenTTL, services.DefaultMaxAuthSessionLifetime)
	}
}

// The configured lifetime has to reach every remembered issue, not only the
// sign-in: a posture re-issue mints through the same helper, and at the floor
// it is also where a lifetime too close to the default would lose the
// remembered-ness.
func TestConfiguredRememberMeTTLReachesSignInAndPostureReissue(t *testing.T) {
	for _, days := range []int{RememberMeTTLDaysMin, RememberMeTTLDaysMax} {
		lifetime := time.Duration(days) * 24 * time.Hour
		t.Run(strconv.Itoa(days)+"d", func(t *testing.T) {
			t.Run("sign-in", func(t *testing.T) {
				app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{rememberMeTTLDays: days})
				email := "remember-ttl-signin-" + strconv.Itoa(days) + "@example.com"
				createOnboardingTestUser(t, database, email, "StrongPass1", true)

				issuedFrom := time.Now()
				response := mustAppResponse(t, app, rememberMeSpecLoginRequest("form", email, true))
				issuedTo := time.Now()
				assertStatusCode(t, response, http.StatusSeeOther)
				assertRememberMeSpecCookie(t, response, true, false, lifetime, issuedFrom, issuedTo)
			})

			// Both re-issue sites: the password change goes through
			// refreshCurrentSession, which every other posture change shares; the
			// recovery-code regeneration seals its own session.
			for _, reissue := range []struct {
				name   string
				method string
				path   string
				form   url.Values
			}{
				{
					name:   "password change",
					method: http.MethodPut,
					path:   "/api/v1/users/current/password",
					form: url.Values{
						"current_password": {"StrongPass1"},
						"new_password":     {"EvenStronger2"},
						"confirm_password": {"EvenStronger2"},
					},
				},
				{
					name:   "recovery code regeneration",
					method: http.MethodPost,
					path:   "/api/v1/users/current/recovery-code",
					form:   url.Values{"password": {"StrongPass1"}},
				},
			} {
				t.Run(reissue.name+" re-issue", func(t *testing.T) {
					app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{enableCSRF: true, rememberMeTTLDays: days})
					email := "remember-ttl-reissue-" + strings.ReplaceAll(reissue.name, " ", "-") + "-" + strconv.Itoa(days) + "@example.com"
					user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
					authCookie, _ := loginWithRememberChoice(t, app, user.Email, "StrongPass1", true)

					csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)
					form := cloneFormValues(reissue.form)
					form.Set("csrf_token", csrfToken)
					request := httptest.NewRequest(reissue.method, reissue.path, strings.NewReader(form.Encode()))
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					request.Header.Set("Cookie", settingsCookieHeader(authCookie, csrfCookie))
					issuedFrom := time.Now()
					response := mustAppResponse(t, app, request)
					issuedTo := time.Now()
					assertRememberMeSpecCookie(t, response, true, false, lifetime, issuedFrom, issuedTo)
				})
			}
		})
	}
}
