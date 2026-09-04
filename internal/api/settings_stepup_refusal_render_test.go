package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Every settings step-up (local-password enrollment, erasure, OIDC identity
// linking) starts on /settings, completes on /auth/oidc/callback, and returns
// the owner to /settings. Its verdict therefore travels in the flash cookie —
// and only ONE of that cookie's channels is read on the way back:
// buildSettingsViewData feeds the view service flash.SettingsSuccess and
// flash.SettingsError, while flash.AuthError is consumed by the auth pages, which
// this redirect never reaches. Flashed on the wrong channel, a refusal renders
// nothing at all: the owner sees the same settings page a success would have
// produced, minus the toast, with no way to tell "your identity is already
// linked elsewhere" from "linked".
//
// The tests below drive each of the three step-ups into a failure arm and then
// render the page the owner actually lands on, because the flash payload alone
// cannot answer whether anything was displayed.

// renderSettingsAfterCallback replays the flash cookie a step-up callback set,
// exactly as the browser would when it follows the 303, and returns the
// rendered settings page.
func renderSettingsAfterCallback(t *testing.T, fixture *oidcStepupFixture, callbackResponse *http.Response) string {
	t.Helper()

	flashCookie := responseCookie(callbackResponse.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected the callback to set a flash cookie carrying the refusal")
	}

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, flashCookie.Name+"="+flashCookie.Value))
	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("settings render after the step-up callback = %d, want 200", response.StatusCode)
	}
	return mustReadBodyString(t, response.Body)
}

func mustEnglishMessage(t *testing.T, key string) string {
	t.Helper()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	message := manager.Messages(i18n.LangEN)[key]
	if strings.TrimSpace(message) == "" {
		t.Fatalf("the en catalogue has no copy for %q", key)
	}
	return message
}

// TestSettingsStepupRefusalsRenderOnTheSettingsPage is the regression for the
// wrong-channel flash. Each case refuses at the exchange, so the request
// reaches the same place the successful case does and diverges exactly at the
// verdict; the assertion is then made on the RENDERED page, not on the cookie,
// since a refusal the page does not read is the defect itself.
func TestSettingsStepupRefusalsRenderOnTheSettingsPage(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		slug string
		// start begins the step-up under test and returns its response, which
		// carries the sealed step-up cookie.
		start func(t *testing.T, fixture *oidcStepupFixture) *http.Response
		// refuse arms the stub so the callback lands in one failure arm.
		refuse         func(fixture *oidcStepupFixture)
		flashKey       string
		translationKey string
	}{
		{
			name: "oidc identity link, identity already claimed",
			slug: "link-claimed",
			start: func(t *testing.T, fixture *oidcStepupFixture) *http.Response {
				t.Helper()
				return postOIDCIdentityLinkStepupStart(t, fixture)
			},
			refuse: func(fixture *oidcStepupFixture) {
				fixture.oidcStub.identityLinkReauthErr = services.ErrOIDCLinkFailed
			},
			flashKey:       settingsOIDCIdentityLinkClaimedErrorSpec().Key,
			translationKey: "settings.error.oidc_identity_already_linked",
		},
		{
			name: "erasure, stale reauth",
			slug: "erasure-stale",
			start: func(t *testing.T, fixture *oidcStepupFixture) *http.Response {
				t.Helper()
				return postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
			},
			refuse: func(fixture *oidcStepupFixture) {
				fixture.oidcStub.reauthErr = services.ErrOIDCReauthStale
			},
			flashKey:       settingsOIDCReauthStaleErrorSpec().Key,
			translationKey: "settings.error.oidc_reauth_stale",
		},
		{
			name: "local password enrollment, identity mismatch",
			slug: "password-mismatch",
			start: func(t *testing.T, fixture *oidcStepupFixture) *http.Response {
				t.Helper()
				return fixture.postStart(t, "EvenStronger2", "EvenStronger2")
			},
			refuse: func(fixture *oidcStepupFixture) {
				fixture.oidcStub.reauthErr = services.ErrOIDCReauthIdentityMismatch
			},
			flashKey:       settingsOIDCReauthMismatchErrorSpec().Key,
			translationKey: "settings.error.oidc_reauth_mismatch",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := newOIDCStepupFixture(t, "settings-stepup-refusal-"+testCase.slug+"@example.com")

			startResponse := testCase.start(t, fixture)
			defer func() { _ = startResponse.Body.Close() }()
			stepupCookie := readStepupCookie(t, startResponse)
			state := extractStepupCallbackState(t, fixture)

			testCase.refuse(fixture)

			callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
			defer func() { _ = callbackResponse.Body.Close() }()
			if callbackResponse.StatusCode != http.StatusSeeOther {
				t.Fatalf("refused step-up callback = %d, want 303 back to /settings", callbackResponse.StatusCode)
			}
			if payload := decodeFlashCookieForTest(t, responseCookie(callbackResponse.Cookies(), flashCookieName).Value); payload.SettingsError != testCase.flashKey {
				t.Fatalf("expected refusal %q on the settings flash channel, got %q (auth channel holds %q)", testCase.flashKey, payload.SettingsError, payload.AuthError)
			}

			body := renderSettingsAfterCallback(t, fixture, callbackResponse)

			if !strings.Contains(body, `data-flash-key="`+testCase.translationKey+`"`) {
				t.Fatalf("the settings page carries no error banner for the refused step-up: expected the stable key %q in the rendered page", testCase.translationKey)
			}
			if message := mustEnglishMessage(t, testCase.translationKey); !strings.Contains(body, message) {
				t.Fatalf("the refusal banner rendered without its copy: expected %q in the rendered page", message)
			}
			// No separate "and no success toast" assertion: resolveSettingsStatusKeys
			// resolves the success flash first and returns before it looks at the
			// error one, so a refusal that reached the page as a success would leave
			// no error banner and fail the check above. It could not fail on its own.
		})
	}
}

// settingsStepupRefusalSpecs derives every spec the three step-up completion
// handlers can flash, by calling the same mappers they call with the same
// sentinels, plus the specs they raise inline. It is a derivation rather than a
// list of keys: a mapper that starts returning a different spec changes this
// set without anything having to be re-typed here.
func settingsStepupRefusalSpecs() []APIErrorSpec {
	foreign := errors.New("some provider failure the mappers do not recognize")

	specs := []APIErrorSpec{
		// Raised inline by completeOIDCIdentityLinkStepup,
		// completeErasureStepupReauth and completeLocalPasswordSetupReauth.
		authOIDCAuthenticationFailedErrorSpec(),
		authOIDCUnavailableErrorSpec(),
		settingsOIDCReauthMismatchErrorSpec(),
		settingsErasureNeedsAccountPasswordErrorSpec(),
		// Raised by applyClearData / applyDeleteAccount once the re-auth passed
		// but the mutation itself failed.
		settingsClearDataErrorSpec(),
		settingsDeleteAccountErrorSpec(),
		authSessionCreateErrorSpec(),
	}
	for _, err := range []error{
		services.ErrOIDCReauthStale,
		services.ErrOIDCLinkFailed,
		services.ErrOIDCDisabled,
		services.ErrOIDCUnavailable,
		services.ErrOIDCIdentityResolveFailed,
		foreign,
	} {
		specs = append(specs, mapOIDCIdentityLinkReauthError(err))
	}
	for _, err := range []error{
		services.ErrOIDCReauthStale,
		services.ErrOIDCReauthIdentityMismatch,
		services.ErrOIDCDisabled,
		services.ErrOIDCUnavailable,
		foreign,
	} {
		specs = append(specs, mapLocalPasswordSetupReauthError(err))
	}
	return specs
}

// TestEverySettingsStepupRefusalKeyMapsToLocalizedCopy is the other half of the
// regression above, which can only drive three arms. Reaching the settings page
// is not enough: resolveSettingsStatusKeys looks the flashed key up in
// services.authErrorTranslationKeys and renders NOTHING when the lookup misses,
// so an unmapped key is the same blank page as the wrong flash channel. This
// walks every spec those handlers can flash and requires copy in every locale —
// the sibling of TestEveryTransportErrorKeyRendersLocalizedCopyInEveryLocale,
// for the step-up surface.
func TestEverySettingsStepupRefusalKeyMapsToLocalizedCopy(t *testing.T) {
	t.Parallel()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	languages := manager.SupportedLanguages()
	if len(languages) == 0 {
		t.Fatal("expected the i18n manager to report supported languages")
	}

	for _, spec := range settingsStepupRefusalSpecs() {
		t.Run(spec.Key, func(t *testing.T) {
			translationKey := services.AuthErrorTranslationKey(spec.Key)
			if translationKey == "" {
				t.Fatalf("step-up refusal %q has no entry in services.authErrorTranslationKeys: the settings page renders an empty banner and the owner cannot tell the refusal from a success", spec.Key)
			}
			for _, language := range languages {
				if message := manager.Messages(language)[translationKey]; strings.TrimSpace(message) == "" {
					t.Errorf("locale %q has no copy for %q (mapped from step-up refusal %q)", language, translationKey, spec.Key)
				}
			}
		})
	}
}
