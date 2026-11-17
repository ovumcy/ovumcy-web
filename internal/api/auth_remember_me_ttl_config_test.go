package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
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

			t.Run("password change re-issue", func(t *testing.T) {
				app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{enableCSRF: true, rememberMeTTLDays: days})
				email := "remember-ttl-reissue-" + strconv.Itoa(days) + "@example.com"
				user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
				authCookie, _ := loginWithRememberChoice(t, app, user.Email, "StrongPass1", true)

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
				issuedFrom := time.Now()
				response := mustAppResponse(t, app, request)
				issuedTo := time.Now()
				assertRememberMeSpecCookie(t, response, true, false, lifetime, issuedFrom, issuedTo)
			})
		})
	}
}
