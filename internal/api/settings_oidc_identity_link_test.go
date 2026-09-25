package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
)

// TestMapOIDCIdentityLinkReauthError unit-tests every branch of the pure
// mapper directly, the same style TestMapOIDCLinkConfirmError already uses:
// cheaper and more precise than driving a full HTTP round-trip for each
// outcome, especially for the cross-user-claim and generic-unavailable arms
// that a step-up flow cannot easily provoke end-to-end.
func TestMapOIDCIdentityLinkReauthError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{name: "stale reauth maps to stale", err: services.ErrOIDCReauthStale, want: settingsOIDCReauthStaleErrorSpec()},
		// Its own key, although it wraps the stale sentinel: on a provider that
		// omits auth_time the stale copy's "try again" is a loop with no exit.
		{name: "missing auth_time maps to its own refusal", err: services.ErrOIDCReauthAuthTimeMissing, want: settingsOIDCReauthAuthTimeMissingErrorSpec()},
		{name: "cross-user claim maps to claimed", err: services.ErrOIDCLinkFailed, want: settingsOIDCIdentityLinkClaimedErrorSpec()},
		{name: "sessions revoked since the step-up began maps to session create", err: services.ErrAuthSessionVersionChanged, want: authSessionCreateErrorSpec()},
		{name: "oidc disabled maps to unavailable", err: services.ErrOIDCDisabled, want: authOIDCUnavailableErrorSpec()},
		{name: "oidc unavailable maps to unavailable", err: services.ErrOIDCUnavailable, want: authOIDCUnavailableErrorSpec()},
		{name: "identity resolve failed maps to unavailable", err: services.ErrOIDCIdentityResolveFailed, want: authOIDCUnavailableErrorSpec()},
		{name: "unknown error falls back to authentication failed", err: errors.New("unmapped exchange error"), want: authOIDCAuthenticationFailedErrorSpec()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mapOIDCIdentityLinkReauthError(tt.err)
			if got.Key != tt.want.Key {
				t.Fatalf("expected error key %q, got %q", tt.want.Key, got.Key)
			}
			if got.Status != tt.want.Status {
				t.Fatalf("expected status %d, got %d", tt.want.Status, got.Status)
			}
		})
	}
}

// End-to-end coverage of the Settings identity-link step-up (issue #701): the
// authenticated replacement for the public /auth/oidc/link-confirm route,
// which stays closed (see auth_oidc_regressions_test.go).
//
// Reuses the oidcStepupFixture built for the local-password step-up: it wires
// a stubbed provider whose reauth verdict the test controls and an
// authenticated owner session, both of which this flow also needs.

const linkFixturePassword = "StrongPass1"

// giveLinkFixtureAPassword turns the fixture's OIDC-only owner into one with a
// local password: linking a new identity requires the current local password
// as fresh proof of the account holder, on top of the provider re-auth.
func giveLinkFixtureAPassword(t *testing.T, fixture *oidcStepupFixture) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(linkFixturePassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash fixture password: %v", err)
	}
	if err := fixture.database.Model(&models.User{}).Where("id = ?", fixture.user.ID).Updates(map[string]any{
		"password_hash":      string(hash),
		"local_auth_enabled": true,
	}).Error; err != nil {
		t.Fatalf("set fixture password: %v", err)
	}
}

func postOIDCIdentityLinkStepupStart(t *testing.T, fixture *oidcStepupFixture) *http.Response {
	t.Helper()
	giveLinkFixtureAPassword(t, fixture)
	return postOIDCIdentityLinkStepupStartWithPassword(t, fixture, linkFixturePassword)
}

func postOIDCIdentityLinkStepupStartWithPassword(t *testing.T, fixture *oidcStepupFixture, password string) *http.Response {
	t.Helper()
	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{"csrf_token": {csrfToken}, "password": {password}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/oidc/link/step-up", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", settingsCookieHeader(fixture.authCookie, csrfCookie))
	return mustAppResponse(t, fixture.app, request)
}

// TestOIDCIdentityLinkStepupStartRefusesWithoutALiveSession pins (a): the
// settings route is behind AuthRequired/OwnerOnly like every other
// usersCurrent route, so a request carrying no session must be refused before
// anything is minted.
func TestOIDCIdentityLinkStepupStartRefusesWithoutALiveSession(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-no-session@example.com")

	// A genuine CSRF cookie+token pair, so the refusal below is provably
	// AuthRequired's and not the CSRF middleware's — the auth cookie is the
	// only thing missing from this request.
	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{"csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/oidc/link/step-up", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", csrfCookie.String())
	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no session, got %d", response.StatusCode)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == oidcStepupCookieName && cookie.Value != "" {
			t.Fatal("expected no step-up cookie to be minted for an unauthenticated request")
		}
	}
	if fixture.oidcStub.lastReauthState != "" {
		t.Fatal("expected no reauth to have been started without a session")
	}
}

// TestOIDCIdentityLinkStepupStartRefusesWhenOIDCIsDisabled covers the branch
// StartOIDCIdentityLinkStepup takes when the provider is not configured at
// all — the same reason the Settings card itself is hidden in that case
// (TestSettingsPageShowsOIDCLinkCardOnlyWhenOIDCIsEnabled), pinned here at
// the endpoint a client could still reach directly.
func TestOIDCIdentityLinkStepupStartRefusesWhenOIDCIsDisabled(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-provider-disabled@example.com")
	fixture.oidcStub.enabled = false

	response := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusOK {
		t.Fatal("expected the start to refuse when the provider is disabled")
	}
	if fixture.oidcStub.lastReauthState != "" {
		t.Fatal("expected no reauth to have been started when the provider is disabled")
	}
}

// TestOIDCIdentityLinkStepupStartSurfacesAProviderFailure covers the branch
// where StartReauth itself fails (provider unreachable): the step-up cookie
// must not be left behind arming a flow the owner can never complete.
func TestOIDCIdentityLinkStepupStartSurfacesAProviderFailure(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-start-failure@example.com")
	fixture.oidcStub.reauthStartErr = services.ErrOIDCUnavailable

	response := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusOK {
		t.Fatal("expected the start to fail when the provider is unreachable")
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == oidcStepupCookieName && cookie.Value != "" {
			t.Fatal("expected no usable step-up cookie after a failed start")
		}
	}
}

// TestOIDCIdentityLinkStepupStartReturnsAnInterstitialForBrowsers covers the
// non-JSON arm: a settings form submit cannot redirect straight to the
// provider, because the page's CSP pins form-action to 'self' across the
// redirect chain.
func TestOIDCIdentityLinkStepupStartReturnsAnInterstitialForBrowsers(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-start-browser@example.com")
	giveLinkFixtureAPassword(t, fixture)

	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{"csrf_token": {csrfToken}, "password": {linkFixturePassword}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/oidc/link/step-up", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Cookie", settingsCookieHeader(fixture.authCookie, csrfCookie))

	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with an interstitial for a browser submit, got %d", response.StatusCode)
	}
	body := mustReadBodyString(t, response.Body)
	if !strings.Contains(body, fixture.oidcStub.reauthURL) {
		t.Fatalf("expected the interstitial to carry the provider URL, got %q", body)
	}
}

// TestOIDCIdentityLinkStepupCallbackRefusesAMismatchedState pins the state
// check on the callback: a step-up cookie presented with someone else's state
// parameter must not authorize the link it names.
func TestOIDCIdentityLinkStepupCallbackRefusesAMismatchedState(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-state-mismatch@example.com")

	startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, "not-the-state-that-was-minted", "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	if fixture.oidcStub.lastConfirmLinkUserID != 0 {
		t.Fatal("a mismatched state must link nothing")
	}
}

// TestOIDCIdentityLinkStepupCallbackRefusesAProviderError covers the arm
// where the provider reports a failure in the callback itself.
func TestOIDCIdentityLinkStepupCallbackRefusesAProviderError(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-provider-error@example.com")

	startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	form := url.Values{"state": {state}, "code": {""}, "error": {"access_denied"}}
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, stepupCookie))
	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if fixture.oidcStub.lastConfirmLinkUserID != 0 {
		t.Fatal("a provider error must link nothing")
	}
}

// TestOIDCIdentityLinkStepupCallbackRefusesWithoutTheStepupFactor pins (b): a
// callback presenting no step-up cookie at all — the case of a session that
// never actually completed the fresh provider re-authentication — must not
// link anything, even carrying a live owner session and a state value the
// provider genuinely issued.
func TestOIDCIdentityLinkStepupCallbackRefusesWithoutTheStepupFactor(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-no-stepup@example.com")

	startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = startResponse.Body.Close() }()
	if startResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from the step-up start, got %d", startResponse.StatusCode)
	}
	state := extractStepupCallbackState(t, fixture)

	// The callback below carries the auth session but deliberately NOT the
	// sealed step-up cookie the start response set — the ordinary-login path
	// (no stepupState at all) is what CompleteOIDCLogin falls through to, and
	// with no matching /auth/oidc/start state cookie either it must refuse.
	form := url.Values{"state": {state}, "code": {"callback-code"}}
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", fixture.authCookie)
	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if fixture.oidcStub.lastConfirmLinkUserID != 0 {
		t.Fatalf("expected ConfirmAndLinkIdentity to never run without the step-up cookie, but it ran for user id %d", fixture.oidcStub.lastConfirmLinkUserID)
	}
}

// TestOIDCIdentityLinkStepupCallbackRefusesAStaleReauth extends (b) to the
// case where the step-up cookie IS presented but the provider's proof of a
// fresh authentication is stale — the same freshness gate the erasure and
// local-password step-ups enforce.
func TestOIDCIdentityLinkStepupCallbackRefusesAStaleReauth(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-stale@example.com")

	startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	fixture.oidcStub.identityLinkReauthErr = services.ErrOIDCReauthStale

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	if fixture.oidcStub.lastConfirmLinkUserID != 0 {
		t.Fatal("a stale reauth must never persist a link")
	}
	flashCookie := responseCookie(callbackResponse.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected a flash cookie carrying the refusal")
	}
	// On SettingsError, not AuthError: /settings reads only the settings channel,
	// so the auth channel would carry this refusal to a page it never reaches.
	if payload := decodeFlashCookieForTest(t, flashCookie.Value); payload.SettingsError != settingsOIDCReauthStaleErrorSpec().Key {
		t.Fatalf("expected the stale-reauth refusal %q on the settings flash channel, got %q (auth channel holds %q)", settingsOIDCReauthStaleErrorSpec().Key, payload.SettingsError, payload.AuthError)
	}
}

// TestOIDCIdentityLinkStepupCallbackRefusesAForeignSession is the identity-
// binding counterpart to the erasure flow's own test of the same name: a
// step-up cookie minted for one owner, presented alongside another owner's
// session, must link nothing.
func TestOIDCIdentityLinkStepupCallbackRefusesAForeignSession(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-foreign-session@example.com")

	startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	form := url.Values{"state": {state}, "code": {"callback-code"}}
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// A session for a DIFFERENT account than the one the step-up cookie names.
	otherUser := createOnboardingTestUser(t, fixture.database, "settings-oidc-link-intruder@example.com", "StrongPass1", true)
	request.Header.Set("Cookie", joinCookieHeader(issueAuthCookieForUser(t, otherUser), stepupCookie))
	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if fixture.oidcStub.lastConfirmLinkUserID != 0 {
		t.Fatal("a step-up presented with another owner's session must link nothing")
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected a flash cookie carrying the refusal")
	}
	if payload := decodeFlashCookieForTest(t, flashCookie.Value); payload.SettingsError != settingsOIDCReauthMismatchErrorSpec().Key {
		t.Fatalf("expected the identity-mismatch refusal %q on the settings flash channel, got %q (auth channel holds %q)", settingsOIDCReauthMismatchErrorSpec().Key, payload.SettingsError, payload.AuthError)
	}
}

// TestOIDCIdentityLinkStepupCompletesAndCreatesTheBinding is the positive
// anchor for (c): a fresh reauth after the start must call
// ConfirmAndLinkIdentity for the SAME account that started the flow, with the
// (issuer, subject) the exchange resolved to — exactly the binding the (now
// closed) public /auth/oidc/link-confirm route used to create.
func TestOIDCIdentityLinkStepupCompletesAndCreatesTheBinding(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-success@example.com")
	fixture.oidcStub.identityLinkClaims = security.OIDCClaims{
		Issuer:  "https://id.example.com",
		Subject: "settings-linked-subject",
	}

	startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = startResponse.Body.Close() }()
	if startResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from the step-up start, got %d", startResponse.StatusCode)
	}
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	if fixture.oidcStub.lastIdentityLinkUserID != fixture.user.ID {
		t.Fatalf("expected CompleteIdentityLinkReauth to run for user %d, got %d", fixture.user.ID, fixture.oidcStub.lastIdentityLinkUserID)
	}
	if want := services.NormalizeAuthSessionVersion(fixture.user.AuthSessionVersion); services.NormalizeAuthSessionVersion(fixture.oidcStub.lastIdentityLinkSessionVersion) != want {
		t.Fatalf("expected the link to revoke from the step-up session's version %d, got %d", want, fixture.oidcStub.lastIdentityLinkSessionVersion)
	}
	fixture.oidcStub.assertIdentityLinkExchangeMatchesStart(t, "callback-code")
	if fixture.oidcStub.lastConfirmLinkUserID != fixture.user.ID {
		t.Fatalf("expected ConfirmAndLinkIdentity to run for user %d, got %d", fixture.user.ID, fixture.oidcStub.lastConfirmLinkUserID)
	}
	if fixture.oidcStub.lastConfirmLinkClaims.Issuer != "https://id.example.com" || fixture.oidcStub.lastConfirmLinkClaims.Subject != "settings-linked-subject" {
		t.Fatalf("unexpected linked claims: %+v", fixture.oidcStub.lastConfirmLinkClaims)
	}

	flashCookie := responseCookie(callbackResponse.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected a success flash cookie")
	}
	if payload := decodeFlashCookieForTest(t, flashCookie.Value); payload.SettingsSuccess != "oidc_identity_linked" {
		t.Fatalf("expected settings_success=oidc_identity_linked, got %q", payload.SettingsSuccess)
	}
	// The link revoked every earlier session in the same write; this device is
	// re-issued one rather than signed out.
	if reissued := responseCookie(callbackResponse.Cookies(), authCookieName); reissued == nil || strings.TrimSpace(reissued.Value) == "" {
		t.Fatal("expected the link to re-issue this device's auth cookie")
	}
}

// TestOIDCIdentityLinkStepupStartRequiresTheAccountPassword is R3's repro: a
// live session alone — what a hijacker holds — must not be able to start the
// flow that binds THEIR provider subject to the account. Without the current
// local password nothing is minted and no provider re-auth begins; the
// positive anchor is TestOIDCIdentityLinkStepupCompletesAndCreatesTheBinding,
// which sends the right password through the same helper.
func TestOIDCIdentityLinkStepupStartRequiresTheAccountPassword(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		withPassword bool
		submitted    string
		wantKey      string
	}{
		"session only, no password":     {withPassword: true, submitted: "", wantKey: settingsMissingPasswordErrorSpec().Key},
		"wrong password":                {withPassword: true, submitted: "WrongPass1", wantKey: settingsInvalidPasswordErrorSpec().Key},
		"account has no local password": {withPassword: false, submitted: "StrongPass1", wantKey: settingsLocalPasswordRequiredErrorSpec().Key},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newOIDCStepupFixture(t, "settings-oidc-link-pw-"+strings.ReplaceAll(name, " ", "-")+"@example.com")
			if tc.withPassword {
				giveLinkFixtureAPassword(t, fixture)
			}
			response := postOIDCIdentityLinkStepupStartWithPassword(t, fixture, tc.submitted)
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode == http.StatusOK {
				t.Fatal("expected the link start to refuse without the account password")
			}
			if body := mustReadBodyString(t, response.Body); !strings.Contains(body, tc.wantKey) {
				t.Fatalf("expected error key %q, got %q", tc.wantKey, body)
			}
			for _, cookie := range response.Cookies() {
				if cookie.Name == oidcStepupCookieName && cookie.Value != "" {
					t.Fatal("expected no step-up cookie without the account password")
				}
			}
			if fixture.oidcStub.lastReauthState != "" {
				t.Fatal("expected no provider re-auth to start without the account password")
			}
		})
	}
}

func deleteOIDCIdentity(t *testing.T, fixture *oidcStepupFixture, identityID string, password string, withCSRF bool) *http.Response {
	t.Helper()
	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{"password": {password}}
	if withCSRF {
		form.Set("csrf_token", csrfToken)
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/current/oidc/identities/"+identityID, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", settingsCookieHeader(fixture.authCookie, csrfCookie))
	return mustAppResponse(t, fixture.app, request)
}

// TestOIDCIdentityUnlinkRequiresCSRFAndThePassword pins the route's gates:
// CSRF at the middleware, then the budgeted password re-auth, before the
// service is ever asked.
func TestOIDCIdentityUnlinkRequiresCSRFAndThePassword(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-unlink-gates@example.com")
	giveLinkFixtureAPassword(t, fixture)
	identityID := strconv.FormatUint(uint64(fixture.identity.ID), 10)

	noCSRF := deleteOIDCIdentity(t, fixture, identityID, linkFixturePassword, false)
	defer func() { _ = noCSRF.Body.Close() }()
	if noCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without a CSRF token, got %d", noCSRF.StatusCode)
	}

	wrong := deleteOIDCIdentity(t, fixture, identityID, "WrongPass1", true)
	defer func() { _ = wrong.Body.Close() }()
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a wrong password, got %d", wrong.StatusCode)
	}

	badID := deleteOIDCIdentity(t, fixture, "not-a-number", linkFixturePassword, true)
	defer func() { _ = badID.Body.Close() }()
	if badID.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for a malformed id, got %d", badID.StatusCode)
	}

	// CodeQL flagged the id parse as an unbounded uint64->uint truncation
	// (converted to `uint` without an upper bound); it now goes through
	// parseRequestUint, which parses at the platform's own uint width. An id
	// past even uint64's range still fails to parse, so it takes the same
	// not-found path a malformed id does rather than truncating into some
	// other owner's identity id.
	overflowID := deleteOIDCIdentity(t, fixture, "99999999999999999999999", linkFixturePassword, true)
	defer func() { _ = overflowID.Body.Close() }()
	if overflowID.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for an id beyond the uint range, got %d", overflowID.StatusCode)
	}

	if fixture.oidcStub.unlinkCalls != 0 {
		t.Fatalf("expected the service never to be asked past a refused gate, got %d calls", fixture.oidcStub.unlinkCalls)
	}
}

// TestOIDCIdentityUnlinkActsForTheSessionOwnerAndReissuesTheSession is the
// positive anchor: the service is asked for the SESSION's account with the
// path's id, and this device is re-issued a session at the bumped version.
func TestOIDCIdentityUnlinkActsForTheSessionOwnerAndReissuesTheSession(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-unlink-success@example.com")
	giveLinkFixtureAPassword(t, fixture)
	identityID := strconv.FormatUint(uint64(fixture.identity.ID), 10)

	response := deleteOIDCIdentity(t, fixture, identityID, linkFixturePassword, true)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.StatusCode, mustReadBodyString(t, response.Body))
	}
	if fixture.oidcStub.lastUnlinkUserID != fixture.user.ID || fixture.oidcStub.lastUnlinkIdentityID != fixture.identity.ID {
		t.Fatalf("expected unlink of identity %d for user %d, got identity %d for user %d",
			fixture.identity.ID, fixture.user.ID, fixture.oidcStub.lastUnlinkIdentityID, fixture.oidcStub.lastUnlinkUserID)
	}
	if reissued := responseCookie(response.Cookies(), authCookieName); reissued == nil || strings.TrimSpace(reissued.Value) == "" {
		t.Fatal("expected the unlink to re-issue this device's auth cookie")
	}
}

// TestOIDCIdentityUnlinkMapsServiceRefusals pins the two owner-facing
// refusals: another owner's (or a missing) id is a 404 with no oracle, and
// removing the last way in is refused with the local-password key.
func TestOIDCIdentityUnlinkMapsServiceRefusals(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err        error
		wantStatus int
	}{
		"not the owner's identity": {err: services.ErrOIDCIdentityNotFound, wantStatus: http.StatusNotFound},
		"last sign-in method":      {err: services.ErrOIDCUnlinkLastSignIn, wantStatus: http.StatusForbidden},
		// mapOIDCIdentityUnlinkError's default arm: an error UnlinkIdentity
		// never documents (a storage fault, say) collapses into the generic
		// SSO-unavailable spec rather than leaking service internals.
		"unmapped service failure": {err: errors.New("oidc store unavailable"), wantStatus: http.StatusServiceUnavailable},
	} {
		fixture := newOIDCStepupFixture(t, "settings-oidc-unlink-"+strings.ReplaceAll(name, " ", "-")+"@example.com")
		giveLinkFixtureAPassword(t, fixture)
		fixture.oidcStub.unlinkErr = tc.err

		response := deleteOIDCIdentity(t, fixture, "4242", linkFixturePassword, true)
		_ = response.Body.Close()
		if response.StatusCode != tc.wantStatus {
			t.Fatalf("%s: expected %d, got %d", name, tc.wantStatus, response.StatusCode)
		}
		if cookie := responseCookie(response.Cookies(), authCookieName); cookie != nil && cookie.Value != "" {
			t.Fatalf("%s: a refused unlink must not re-issue a session", name)
		}
	}
}

// TestOIDCIdentityUnlinkRefusesWhenOIDCIsDisabled covers the branch
// UnlinkOIDCIdentity takes before it ever parses the id or asks for the
// account password: a provider that is not configured refuses immediately,
// the same posture StartOIDCIdentityLinkStepup takes
// (TestOIDCIdentityLinkStepupStartRefusesWhenOIDCIsDisabled).
func TestOIDCIdentityUnlinkRefusesWhenOIDCIsDisabled(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-unlink-disabled@example.com")
	fixture.oidcStub.enabled = false

	response := deleteOIDCIdentity(t, fixture, "1", linkFixturePassword, true)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected %d when the provider is disabled, got %d", http.StatusServiceUnavailable, response.StatusCode)
	}
	if fixture.oidcStub.unlinkCalls != 0 {
		t.Fatal("expected the service never to be asked when OIDC is disabled")
	}
}

// deleteOIDCIdentityWithFormat is deleteOIDCIdentity with the response format
// under the caller's control, for the two success arms a JSON caller never
// exercises: the HTMX redirect and the plain browser redirect.
func deleteOIDCIdentityWithFormat(t *testing.T, fixture *oidcStepupFixture, identityID, password, accept string, htmx bool) *http.Response {
	t.Helper()
	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{"csrf_token": {csrfToken}, "password": {password}}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/current/oidc/identities/"+identityID, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", accept)
	if htmx {
		request.Header.Set("HX-Request", "true")
	}
	request.Header.Set("Cookie", settingsCookieHeader(fixture.authCookie, csrfCookie))
	return mustAppResponse(t, fixture.app, request)
}

// TestOIDCIdentityUnlinkHTMXSuccessRedirectsViaHXRedirect and
// TestOIDCIdentityUnlinkBrowserSuccessRedirectsToSettings are the two success
// arms TestOIDCIdentityUnlinkActsForTheSessionOwnerAndReissuesTheSession never
// reaches: that anchor always asks for JSON, so acceptsJSON(c) answers first
// and short-circuits both.
func TestOIDCIdentityUnlinkHTMXSuccessRedirectsViaHXRedirect(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-unlink-htmx@example.com")
	giveLinkFixtureAPassword(t, fixture)
	identityID := strconv.FormatUint(uint64(fixture.identity.ID), 10)

	response := deleteOIDCIdentityWithFormat(t, fixture, identityID, linkFixturePassword, "text/html", true)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with HX-Redirect on an HTMX unlink, got %d", response.StatusCode)
	}
	if redirect := response.Header.Get("HX-Redirect"); redirect != "/settings" {
		t.Fatalf("expected HX-Redirect /settings, got %q", redirect)
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected a success flash cookie on the HTMX unlink")
	}
}

func TestOIDCIdentityUnlinkBrowserSuccessRedirectsToSettings(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-unlink-browser@example.com")
	giveLinkFixtureAPassword(t, fixture)
	identityID := strconv.FormatUint(uint64(fixture.identity.ID), 10)

	response := deleteOIDCIdentityWithFormat(t, fixture, identityID, linkFixturePassword, "text/html", false)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect for a plain browser unlink, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", location)
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected a success flash cookie on the plain unlink")
	}
}

// TestOIDCIdentityUnlinkReissueFailureIsReportedAsARefusal pins the same seam
// TestApplyClearDataReportsARefusedSessionReissueToItsCaller pins for
// clear-data: reissueSessionAfterIdentityChange's !ok branch has to reach the
// caller as a refusal, not be swallowed into the success response the unlink
// already wrote up to that point.
//
// The refusal is provoked the same way, with a role
// reissueSessionAfterIdentityChange's own fresh FindByID will refuse but that
// no registered route can carry into the handler in the first place
// (AuthRequired resolves the same role first and would refuse the request
// before UnlinkOIDCIdentity ever runs). The probe therefore injects the
// session user directly via Locals — the same seam UnlinkOIDCIdentity reads
// through currentUser — instead of going through AuthRequired.
func TestOIDCIdentityUnlinkReissueFailureIsReportedAsARefusal(t *testing.T) {
	t.Parallel()

	// The cookie is already cleared by the time the refusal is answered, so
	// every page-bound format lands on /login; JSON keeps the mapped envelope.
	for _, testCase := range []struct {
		name   string
		accept string
		htmx   bool
	}{
		{name: "json", accept: "application/json"},
		{name: "htmx", accept: "text/html", htmx: true},
		{name: "html", accept: "text/html"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			response, stub, userID := probeRefusedUnlinkReissue(t, testCase.name, testCase.accept, testCase.htmx)
			defer func() { _ = response.Body.Close() }()

			// authIdentityChangeAppliedSignInAgainErrorSpec, not
			// authWebSignInUnavailableErrorSpec (refreshCurrentSession's own
			// internal spec): the unlink already committed before the reissue was
			// asked, so "failed to create session" would tell the owner nothing
			// happened, which is false. See reissueSessionAfterIdentityChange.
			wantSpec := authIdentityChangeAppliedSignInAgainErrorSpec()
			if testCase.accept == "application/json" {
				if response.StatusCode != wantSpec.Status {
					t.Fatalf("expected the refused reissue's status %d, got %d: %s", wantSpec.Status, response.StatusCode, mustReadBodyString(t, response.Body))
				}
				if body := mustReadBodyString(t, response.Body); !strings.Contains(body, wantSpec.Key) {
					t.Fatalf("expected the refused-reissue key %q in the body, got %q", wantSpec.Key, body)
				}
			} else {
				assertSignedOutRefusal(t, response, wantSpec.Key, testCase.htmx)
			}
			// Anti-vacuity: the unlink itself ran before the reissue was asked, so
			// a refusal above can only have come from the reissue and not from the
			// service call being skipped.
			if stub.unlinkCalls != 1 || stub.lastUnlinkUserID != userID {
				t.Fatalf("expected UnlinkIdentity to have run for user %d before the reissue, got %d calls for user %d", userID, stub.unlinkCalls, stub.lastUnlinkUserID)
			}
		})
	}
}

// probeRefusedUnlinkReissue runs UnlinkOIDCIdentity for a "partner" row in the
// given format and returns the response, the stub it called, and the row's id.
func probeRefusedUnlinkReissue(t *testing.T, slug, accept string, htmx bool) (*http.Response, *stubOIDCWorkflowService, uint) {
	t.Helper()

	stub := newStubOIDCWorkflowService(true)
	// newSettingsMutationStepupApp's own app ends in a catch-all
	// app.Use(handler.NotFound) registered after every production route, so a
	// route added to it here — after the helper returns — would never be
	// reached; the catch-all answers first. The probe therefore takes only the
	// handler and database from the fixture and mounts its single route on a
	// bare app of its own, the same split probeApplyClearData uses.
	_, database, handler := newSettingsMutationStepupApp(t, stub)
	app := fiber.New()

	password := "StrongPass1"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash probe password: %v", err)
	}
	user := models.User{
		Email:            "oidc-unlink-reissue-refused-" + slug + "@example.com",
		LocalAuthEnabled: true,
		PasswordHash:     string(hash),
		// The one non-owner value the users table's CHECK constraint still
		// accepts, which is what makes reissueSessionAfterIdentityChange's
		// role gate reachable from a row a test can create, the same
		// technique TestApplyClearDataReportsARefusedSessionReissueToItsCaller
		// uses.
		Role:                "partner",
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create the probe account: %v", err)
	}

	app.Delete("/__probe/oidc-unlink/:id", func(c fiber.Ctx) error {
		c.Locals(contextUserKey, &user)
		return handler.UnlinkOIDCIdentity(c)
	})

	form := url.Values{"password": {password}}
	request := httptest.NewRequest(http.MethodDelete, "/__probe/oidc-unlink/1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", accept)
	if htmx {
		request.Header.Set("HX-Request", "true")
	}
	return mustAppResponse(t, app, request), stub, user.ID
}

// TestOIDCIdentityLinkStepupReissueFailureIsReportedAsARefusal is the
// identity-link counterpart above: reissueSessionAfterIdentityChange's !ok
// branch inside completeOIDCIdentityLinkStepup must redirect the refusal, not
// the success the flash cookie would otherwise carry.
//
// completeOIDCIdentityLinkStepup re-resolves the session from the real auth
// cookie (authenticateRequest) before it ever asks the service to confirm the
// link, and the reissue's own FindByID re-resolves the same row again
// afterward. A role flip made before the call would already fail that FIRST
// read, so the Locals-injection probe the unlink test above uses cannot reach
// this arm. Instead the stub's afterIdentityLinkConfirm hook flips the row's
// role in the gap between the two reads: the account passes the first read as
// an owner and only becomes unsupported for the second one, which is exactly
// the reissue failure this test exists to pin.
func TestOIDCIdentityLinkStepupReissueFailureIsReportedAsARefusal(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-reissue-failure@example.com")
	fixture.oidcStub.identityLinkClaims = security.OIDCClaims{
		Issuer:  "https://id.example.com",
		Subject: "reissue-failure-subject",
	}
	fixture.oidcStub.afterIdentityLinkConfirm = func() {
		if err := fixture.database.Model(&models.User{}).Where("id = ?", fixture.user.ID).Update("role", "partner").Error; err != nil {
			t.Fatalf("flip the fixture role to provoke a refused reissue: %v", err)
		}
	}

	startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	// Anti-vacuity: the link itself completed before the reissue ran, so a
	// refusal below can only have come from the reissue and not from
	// ConfirmAndLinkIdentity never being asked. The stub writes nothing, so
	// this is "the identity row is present after the link" for a fake service.
	if fixture.oidcStub.lastConfirmLinkUserID != fixture.user.ID {
		t.Fatalf("expected the link to have been confirmed before the reissue ran, got user id %d", fixture.oidcStub.lastConfirmLinkUserID)
	}
	// Not redirectSettingsRefusal: completeOIDCIdentityLinkStepup's reissue
	// failure arm has already cleared the auth cookie, so it flashes AuthError
	// and lands on /login directly rather than /settings, which would bounce
	// there anyway and lose the flash. See redirectSignedOutRefusal.
	if callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected the refused reissue to redirect, got %d", callbackResponse.StatusCode)
	}
	if location := callbackResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected the refused reissue to land on /login, got %q", location)
	}
	flashCookie := responseCookie(callbackResponse.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected a flash cookie carrying the refused reissue")
	}
	// authIdentityChangeAppliedSignInAgainErrorSpec, not
	// authWebSignInUnavailableErrorSpec: the link already committed, so the
	// generic session-create refusal would tell the owner nothing happened,
	// which is false.
	wantKey := authIdentityChangeAppliedSignInAgainErrorSpec().Key
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != wantKey {
		t.Fatalf("expected the refused-reissue key %q on the auth flash channel (the one /login reads), got AuthError=%q SettingsError=%q", wantKey, payload.AuthError, payload.SettingsError)
	}
}

// TestOIDCIdentityLinkStepupVersionRaceDuringReissueIsReportedAsARefusal is
// TestOIDCIdentityLinkStepupReissueFailureIsReportedAsARefusal's sibling for
// reissueSessionAfterIdentityChange's OTHER failure arm: not refreshCurrentSession
// refusing the re-mint, but the version-mismatch branch, where a second write
// moves auth_session_version again between the link's commit and this reload.
// The stub's afterIdentityLinkConfirm hook lands the race in that exact gap,
// the same technique the sibling test uses for its own gap.
func TestOIDCIdentityLinkStepupVersionRaceDuringReissueIsReportedAsARefusal(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-version-race@example.com")
	fixture.oidcStub.identityLinkClaims = security.OIDCClaims{
		Issuer:  "https://id.example.com",
		Subject: "version-race-subject",
	}
	fixture.oidcStub.afterIdentityLinkConfirm = func() {
		// A concurrent write (another device signing out everywhere, a second
		// posture change) bumps the version again right after the link
		// committed but before reissueSessionAfterIdentityChange's reload sees
		// it — the pre-existing revocation has to win over this device's link.
		if err := fixture.database.Model(&models.User{}).
			Where("id = ?", fixture.user.ID).
			UpdateColumn("auth_session_version", fixture.user.AuthSessionVersion+2).Error; err != nil {
			t.Fatalf("bump the fixture's version to provoke the race: %v", err)
		}
	}

	startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	// Anti-vacuity: the link itself completed before the race was provoked, so
	// the refusal below can only have come from the version-mismatch arm.
	if fixture.oidcStub.lastConfirmLinkUserID != fixture.user.ID {
		t.Fatalf("expected the link to have been confirmed before the race ran, got user id %d", fixture.oidcStub.lastConfirmLinkUserID)
	}
	if callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected the refused reissue to redirect, got %d", callbackResponse.StatusCode)
	}
	if location := callbackResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected the version-race refusal to land on /login, got %q", location)
	}
	flashCookie := responseCookie(callbackResponse.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected a flash cookie carrying the version-race refusal")
	}
	wantKey := authIdentityChangeAppliedSignInAgainErrorSpec().Key
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != wantKey {
		t.Fatalf("expected the refused-reissue key %q on the auth flash channel, got AuthError=%q SettingsError=%q", wantKey, payload.AuthError, payload.SettingsError)
	}
	// The session must not survive the race: this device's own cookie was
	// cleared by refuseSessionRevokedDuring, exactly as for every other
	// sign-out-everywhere event it answers.
	authCookie := responseCookie(callbackResponse.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("expected the auth cookie to be cleared after the version race, got %v", authCookie)
	}
}

// TestSettingsPageRefusesWhenListingLinkedIdentitiesFails pins
// buildSettingsViewData's OIDC branch: a ListLinkedIdentities failure (a
// storage fault, unlike every other arm in this file which refuses a
// request rather than a page load) must surface as the page's own load
// failure rather than a half-built page that silently drops the
// linked-identities list.
func TestSettingsPageRefusesWhenListingLinkedIdentitiesFails(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-list-failure@example.com")
	fixture.oidcStub.enabled = true
	fixture.oidcStub.listLinkedErr = errors.New("oidc identity store unavailable")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", fixture.authCookie)
	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected the settings page to refuse when listing linked identities fails, got %d", response.StatusCode)
	}
}
