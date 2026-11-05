package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestSettingsStepupRefusalsLandOnSettings pins where the SSO refusals the
// Settings step-up starts and the unlink share with sign-in render (WEB-490,
// the siblings of WEB-483): a browser form submit gets the /settings flash and
// never the sign-in page's channel, and an HTMX submit gets the 200 status
// fragment.
func TestSettingsStepupRefusalsLandOnSettings(t *testing.T) {
	t.Parallel()

	const (
		linkStart   = "/api/v1/users/current/oidc/link/step-up"
		wipeStart   = "/api/v1/users/current/data-wipe/step-up"
		deleteStart = "/api/v1/users/current/deletion/step-up"
		unlink      = "/api/v1/users/current/oidc/identities/4242"
	)
	unmapped := errors.New("unmapped provider error")

	refusals := map[string]struct {
		method      string
		path        string
		password    bool
		disabled    bool
		reauthStart error
		unlinkErr   error
		want        string
	}{
		"link start, oidc disabled":           {method: http.MethodPost, path: linkStart, password: true, disabled: true, want: "sso temporarily unavailable"},
		"link start, provider unreachable":    {method: http.MethodPost, path: linkStart, password: true, reauthStart: services.ErrOIDCUnavailable, want: "sso temporarily unavailable"},
		"link start, unmapped provider error": {method: http.MethodPost, path: linkStart, password: true, reauthStart: unmapped, want: "sso authentication failed"},
		"unlink, oidc disabled":               {method: http.MethodDelete, path: unlink, password: true, disabled: true, want: "sso temporarily unavailable"},
		"unlink, unmapped service failure":    {method: http.MethodDelete, path: unlink, password: true, unlinkErr: unmapped, want: "sso temporarily unavailable"},
		"data wipe start, oidc disabled":      {method: http.MethodPost, path: wipeStart, disabled: true, want: "sso temporarily unavailable"},
		"data wipe start, provider failure":   {method: http.MethodPost, path: wipeStart, reauthStart: services.ErrOIDCUnavailable, want: "sso temporarily unavailable"},
		"deletion start, oidc disabled":       {method: http.MethodPost, path: deleteStart, disabled: true, want: "sso temporarily unavailable"},
		"deletion start, unmapped failure":    {method: http.MethodPost, path: deleteStart, reauthStart: unmapped, want: "sso authentication failed"},
	}
	for name, tc := range refusals {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := newOIDCStepupFixture(t, "settings-stepup-refusal-"+strings.NewReplacer(" ", "-", ",", "").Replace(name)+"@example.com")
			if tc.password {
				giveLinkFixtureAPassword(t, fixture)
			}
			fixture.oidcStub.enabled = !tc.disabled
			fixture.oidcStub.reauthStartErr = tc.reauthStart
			fixture.oidcStub.unlinkErr = tc.unlinkErr

			send := func(htmx bool) *http.Response {
				csrfCookie, csrfToken := fixture.settingsCSRF(t)
				form := url.Values{"csrf_token": {csrfToken}, dayFormAccountField: {renderedDayFormAccount(t, fixture.app, fixture.authCookie, "/settings")}}
				if tc.password {
					form.Set("password", linkFixturePassword)
				}
				request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if htmx {
					request.Header.Set("HX-Request", "true")
				} else {
					request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
				}
				request.Header.Set("Cookie", settingsCookieHeader(fixture.authCookie, csrfCookie))
				return mustAppResponse(t, fixture.app, request)
			}

			response := send(false)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/settings" {
				t.Fatalf("expected a 303 to /settings, got %d to %q", response.StatusCode, response.Header.Get("Location"))
			}
			flashCookie := responseCookie(response.Cookies(), flashCookieName)
			if flashCookie == nil {
				t.Fatal("expected a refusal flash")
			}
			if payload := decodeFlashCookieForTest(t, flashCookie.Value); payload.SettingsError != tc.want || payload.AuthError != "" {
				t.Fatalf("expected settings error %q and no auth error, got settings=%q auth=%q", tc.want, payload.SettingsError, payload.AuthError)
			}

			htmx := send(true)
			body := mustReadBodyString(t, htmx.Body)
			flashKey := `data-flash-key="` + services.AuthErrorTranslationKey(tc.want) + `"`
			if htmx.StatusCode != http.StatusOK || !strings.Contains(body, flashKey) {
				t.Fatalf("expected an HTMX status fragment carrying %s, got %d: %q", flashKey, htmx.StatusCode, body)
			}
		})
	}
}

// TestSettingsStepupSpecsNeverTargetTheAuthForm pins the mapper the unlink
// answers through: no arm may aim at a sign-in form a Settings route lacks.
func TestSettingsStepupSpecsNeverTargetTheAuthForm(t *testing.T) {
	t.Parallel()

	for _, err := range []error{services.ErrOIDCIdentityNotFound, services.ErrOIDCUnlinkLastSignIn, errors.New("unmapped")} {
		if got := mapOIDCIdentityUnlinkError(err); got.Target == APIErrorTargetAuthForm {
			t.Fatalf("%v: expected no auth-form target on the unlink, got %q", err, got.Target)
		}
	}
	if got := settingsOIDCUnavailableErrorSpec(); got.Target != APIErrorTargetSettingsForm {
		t.Fatalf("expected the settings form, got %q", got.Target)
	}
}
