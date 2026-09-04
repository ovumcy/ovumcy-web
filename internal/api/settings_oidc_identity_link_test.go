package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
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
		{name: "cross-user claim maps to claimed", err: services.ErrOIDCLinkFailed, want: settingsOIDCIdentityLinkClaimedErrorSpec()},
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

func postOIDCIdentityLinkStepupStart(t *testing.T, fixture *oidcStepupFixture) *http.Response {
	t.Helper()
	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{"csrf_token": {csrfToken}}
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

	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{"csrf_token": {csrfToken}}
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
	if payload := decodeFlashCookieForTest(t, flashCookie.Value); payload.AuthError != settingsOIDCReauthStaleErrorSpec().Key {
		t.Fatalf("expected the stale-reauth refusal %q, got %q", settingsOIDCReauthStaleErrorSpec().Key, payload.AuthError)
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
	if payload := decodeFlashCookieForTest(t, flashCookie.Value); payload.AuthError != settingsOIDCReauthMismatchErrorSpec().Key {
		t.Fatalf("expected the identity-mismatch refusal %q, got %q", settingsOIDCReauthMismatchErrorSpec().Key, payload.AuthError)
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
}
