package api

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
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
		passwordStart = "/api/v1/users/current/password/step-up"
		linkStart     = "/api/v1/users/current/oidc/link/step-up"
		wipeStart     = "/api/v1/users/current/data-wipe/step-up"
		deleteStart   = "/api/v1/users/current/deletion/step-up"
		unlink        = "/api/v1/users/current/oidc/identities/4242"
	)
	unmapped := errors.New("unmapped provider error")

	refusals := map[string]struct {
		method      string
		path        string
		password    bool
		newPassword bool
		bound       bool
		disabled    bool
		reauthStart error
		unlinkErr   error
		want        string
	}{
		"password start, oidc disabled":       {method: http.MethodPost, path: passwordStart, newPassword: true, disabled: true, want: "sso temporarily unavailable"},
		"password start, provider failure":    {method: http.MethodPost, path: passwordStart, newPassword: true, reauthStart: services.ErrOIDCUnavailable, want: "sso temporarily unavailable"},
		"password start, unmapped failure":    {method: http.MethodPost, path: passwordStart, newPassword: true, reauthStart: unmapped, want: "sso authentication failed"},
		"link start, oidc disabled":           {method: http.MethodPost, path: linkStart, password: true, disabled: true, want: "sso temporarily unavailable"},
		"link start, provider unreachable":    {method: http.MethodPost, path: linkStart, password: true, reauthStart: services.ErrOIDCUnavailable, want: "sso temporarily unavailable"},
		"link start, unmapped provider error": {method: http.MethodPost, path: linkStart, password: true, reauthStart: unmapped, want: "sso authentication failed"},
		"unlink, oidc disabled":               {method: http.MethodDelete, path: unlink, password: true, disabled: true, want: "sso temporarily unavailable"},
		"unlink, unmapped service failure":    {method: http.MethodDelete, path: unlink, password: true, unlinkErr: unmapped, want: "sso temporarily unavailable"},
		"data wipe start, oidc disabled":      {method: http.MethodPost, path: wipeStart, bound: true, disabled: true, want: "sso temporarily unavailable"},
		"data wipe start, provider failure":   {method: http.MethodPost, path: wipeStart, bound: true, reauthStart: services.ErrOIDCUnavailable, want: "sso temporarily unavailable"},
		"deletion start, oidc disabled":       {method: http.MethodPost, path: deleteStart, bound: true, disabled: true, want: "sso temporarily unavailable"},
		"deletion start, unmapped failure":    {method: http.MethodPost, path: deleteStart, bound: true, reauthStart: unmapped, want: "sso authentication failed"},
	}
	for name, tc := range refusals {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := newOIDCStepupFixture(t, "settings-stepup-refusal-"+strings.NewReplacer(" ", "-", ",", "").Replace(name)+"@example.com")
			if tc.password {
				giveLinkFixtureAPassword(t, fixture)
			}
			csrfCookie, csrfToken := fixture.settingsCSRF(t)
			form := url.Values{"csrf_token": {csrfToken}}
			if tc.password {
				form.Set("password", linkFixturePassword)
			}
			if tc.newPassword {
				form.Set("new_password", "FreshStrong2")
				form.Set("confirm_password", "FreshStrong2")
			}
			if tc.bound {
				// The erasure starts refuse a rendered-page form that does not
				// carry the account binding before the handler runs.
				form.Set(dayFormAccountField, renderedDayFormAccount(t, fixture.app, fixture.authCookie, "/settings"))
			}
			fixture.oidcStub.enabled = !tc.disabled
			fixture.oidcStub.reauthStartErr = tc.reauthStart
			fixture.oidcStub.unlinkErr = tc.unlinkErr

			send := func(htmx bool) *http.Response {
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

// TestSettingsStepupHandlersRaiseNoSignInSpec covers the arms the route test
// cannot reach — the step-up callbacks, whose redirectSettingsRefusal carries
// any spec to /settings whatever its target, and the codecov-ignored guards: a
// handler file that raises a sign-in SSO spec again fails here by name.
func TestSettingsStepupHandlersRaiseNoSignInSpec(t *testing.T) {
	t.Parallel()

	signIn := map[string]bool{
		"authOIDCUnavailableErrorSpec":          true,
		"authOIDCAuthenticationFailedErrorSpec": true,
		"authOIDCAccountUnavailableErrorSpec":   true,
		"mapAuthOIDCError":                      true,
	}
	for _, file := range []string{
		"handlers_settings_password.go",
		"handlers_settings_oidc_link.go",
		"handlers_settings_danger_stepup.go",
		"oidc_stepup_continuation.go",
	} {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && signIn[ident.Name] {
				t.Errorf("%s calls %s: a Settings step-up answers through the settings-form specs (WEB-490)", file, ident.Name)
			}
			return true
		})
	}
}

// TestSettingsStepupSpecsNeverTargetTheAuthForm pins the mappers the routes
// above and the step-up callbacks answer through: no arm may aim at a sign-in
// form a Settings route lacks.
func TestSettingsStepupSpecsNeverTargetTheAuthForm(t *testing.T) {
	t.Parallel()

	specs := map[string]APIErrorSpec{
		"unavailable":           settingsOIDCUnavailableErrorSpec(),
		"authentication failed": settingsOIDCAuthenticationFailedErrorSpec(),
	}
	for _, err := range []error{
		services.ErrOIDCDisabled,
		services.ErrOIDCUnavailable,
		services.ErrOIDCAccountUnavailable,
		services.ErrOIDCIdentityResolveFailed,
		services.ErrOIDCReauthStale,
		services.ErrOIDCReauthAuthTimeMissing,
		services.ErrOIDCReauthIdentityMismatch,
		services.ErrOIDCIdentityNotFound,
		services.ErrOIDCUnlinkLastSignIn,
		errors.New("unmapped"),
	} {
		specs["start "+err.Error()] = mapSettingsOIDCError(err)
		specs["callback "+err.Error()] = mapLocalPasswordSetupReauthError(err)
		specs["unlink "+err.Error()] = mapOIDCIdentityUnlinkError(err)
	}
	for name, spec := range specs {
		if spec.Target == APIErrorTargetAuthForm {
			t.Errorf("%s: expected no auth-form target on a Settings step-up, got %q (key %q)", name, spec.Target, spec.Key)
		}
	}
}
