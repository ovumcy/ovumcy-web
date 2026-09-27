package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
)

type stubOIDCWorkflowService struct {
	enabled bool
	// providerLogoutDisabled stands for OIDC_LOGOUT_MODE=local on an instance
	// whose OIDC is otherwise on: the write-time mode and the read-time mode
	// are the same predicate, so a test that switches it observes what a
	// stored row is worth after the switch.
	providerLogoutDisabled bool
	localPublicAuthEnabled bool
	responseMode           security.OIDCResponseMode
	issuerURL              string
	postLogoutRedirectURL  string
	authURL                string
	startErr               error
	result                 services.OIDCLoginResult
	authErr                error
	lastStartState         string
	lastStartNonce         string
	lastStartVerifier      string
	lastStartDeadline      time.Time
	lastAuthCode           string
	lastAuthVerifier       string
	lastAuthExpectedNonce  string
	lastAuthDeadline       time.Time
	reauthURL              string
	reauthStartErr         error
	reauthErr              error
	lastReauthState        string
	lastReauthNonce        string
	lastReauthVerifier     string
	lastReauthCode         string
	lastReauthCodeVerifier string
	lastReauthNonceCheck   string
	lastReauthUserID       uint
	lastReauthMaxAge       time.Duration
	confirmLinkErr         error
	unlinkErr              error
	unlinkCalls            int
	lastUnlinkUserID       uint
	lastUnlinkIdentityID   uint
	linkedIdentities       []services.LinkedOIDCIdentity
	listLinkedErr          error
	lastConfirmLinkUserID  uint
	lastConfirmLinkClaims  security.OIDCClaims

	// Identity-link step-up (Settings). identityLinkReauthErr, when set, is
	// returned by CompleteIdentityLinkReauth directly (simulating an exchange
	// or freshness failure) without ever reaching ConfirmAndLinkIdentity.
	// Otherwise the stub records the call and falls through to confirmLinkErr,
	// mirroring the real method's "exchange, then ConfirmAndLinkIdentity" shape.
	identityLinkReauthErr        error
	identityLinkClaims           security.OIDCClaims
	lastIdentityLinkCode         string
	lastIdentityLinkCodeVerifier string
	lastIdentityLinkNonce        string
	lastIdentityLinkUserID       uint
	lastIdentityLinkMaxAge       time.Duration

	lastIdentityLinkSessionVersion int

	// afterIdentityLinkConfirm, when set, runs once ConfirmAndLinkIdentity's
	// stand-in has recorded the call and right before the stub returns — the
	// same point the real service method hands control back to
	// completeOIDCIdentityLinkStepup, just before it re-issues the session. A
	// test uses this to mutate the account's row (e.g. flip its role)
	// in the gap between the handler's own authenticateRequest read and
	// reissueSessionAfterIdentityChange's later FindByID, which is otherwise
	// unreachable from outside a single synchronous handler call.
	afterIdentityLinkConfirm func()
}

func (stub *stubOIDCWorkflowService) Enabled() bool {
	return stub.enabled
}

func (stub *stubOIDCWorkflowService) LocalPublicAuthEnabled() bool {
	if !stub.enabled {
		return true
	}
	if !stub.localPublicAuthEnabled {
		return false
	}
	return true
}

func (stub *stubOIDCWorkflowService) ResponseMode() security.OIDCResponseMode {
	if stub.responseMode == "" {
		return security.OIDCResponseModeFormPost
	}
	return stub.responseMode
}

// IssuerURL is the origin stored provider-logout state is pinned to; a test
// that drives a valid logout state names the issuer its endpoint sits on.
func (stub *stubOIDCWorkflowService) IssuerURL() string {
	return stub.issuerURL
}

// PostLogoutRedirectURL is the configured post-logout return address the
// provider redirect is composed from.
func (stub *stubOIDCWorkflowService) PostLogoutRedirectURL() string {
	return stub.postLogoutRedirectURL
}

// ProviderLogoutEnabled is the mode in force at sign-out time. An enabled stub
// reports provider logout on unless a test turns it off, which is what the
// tests around the bridge assume; a disabled stub reports it off, exactly as
// the real service does when OIDC is off.
func (stub *stubOIDCWorkflowService) ProviderLogoutEnabled() bool {
	return stub.enabled && !stub.providerLogoutDisabled
}

func (stub *stubOIDCWorkflowService) StartAuth(ctx context.Context, state string, nonce string, codeVerifier string) (string, error) {
	stub.lastStartState = state
	stub.lastStartNonce = nonce
	stub.lastStartVerifier = codeVerifier
	if deadline, ok := ctx.Deadline(); ok {
		stub.lastStartDeadline = deadline
	}
	if stub.startErr != nil {
		return "", stub.startErr
	}
	return stub.authURL, nil
}

func (stub *stubOIDCWorkflowService) Authenticate(ctx context.Context, code string, codeVerifier string, expectedNonce string, _ time.Time) (services.OIDCLoginResult, error) {
	stub.lastAuthCode = code
	stub.lastAuthVerifier = codeVerifier
	stub.lastAuthExpectedNonce = expectedNonce
	if deadline, ok := ctx.Deadline(); ok {
		stub.lastAuthDeadline = deadline
	}
	if stub.authErr != nil {
		// The real OIDC service returns both the populated result and the
		// ErrOIDCLinkRequiresConfirmation error so the handler can hand off
		// to the password-confirmation step with the pending-link payload.
		// Mirror that contract here; for every other error the result stays
		// zero.
		if errors.Is(stub.authErr, services.ErrOIDCLinkRequiresConfirmation) {
			return stub.result, stub.authErr
		}
		return services.OIDCLoginResult{}, stub.authErr
	}
	return stub.result, nil
}

func (stub *stubOIDCWorkflowService) StartReauth(_ context.Context, state string, nonce string, codeVerifier string) (string, error) {
	stub.lastReauthState = state
	stub.lastReauthNonce = nonce
	stub.lastReauthVerifier = codeVerifier
	if stub.reauthStartErr != nil {
		return "", stub.reauthStartErr
	}
	if stub.reauthURL != "" {
		return stub.reauthURL, nil
	}
	return stub.authURL, nil
}

func (stub *stubOIDCWorkflowService) ValidateReauthExchange(_ context.Context, code string, codeVerifier string, expectedNonce string, expectedUserID uint, maxAuthAge time.Duration, _ time.Time) error {
	stub.lastReauthCode = code
	stub.lastReauthCodeVerifier = codeVerifier
	stub.lastReauthNonceCheck = expectedNonce
	stub.lastReauthUserID = expectedUserID
	stub.lastReauthMaxAge = maxAuthAge
	return stub.reauthErr
}

// UnlinkIdentity records what the handler asked for and answers unlinkErr.
// The handler's own gates (password, id parse) are what the api tests pin;
// the service rules live in internal/services. The stub writes nothing, so the
// session version it reports is the one the account still holds.
func (stub *stubOIDCWorkflowService) UnlinkIdentity(_ context.Context, user models.User, identityID uint) (int, error) {
	stub.unlinkCalls++
	stub.lastUnlinkUserID = user.ID
	stub.lastUnlinkIdentityID = identityID
	if stub.unlinkErr != nil {
		return 0, stub.unlinkErr
	}
	return services.NormalizeAuthSessionVersion(user.AuthSessionVersion), nil
}

// assertStepupExchangeMatchesStart pins that a step-up completion validated
// the provider answer against the values ITS OWN start minted: the code the
// callback carried, the PKCE verifier and nonce the start handed to StartReauth
// (read back from the sealed state cookie), and a non-zero max-age. A handler
// that validated with a blank or a different verifier/nonce — or dropped the
// max-age — would still reach its success path against the stub, so the
// comparison has to be made here, on every step-up purpose.
func assertStepupExchangeMatchesStart(t *testing.T, stub *stubOIDCWorkflowService, wantCode string, gotCode string, gotVerifier string, gotNonce string, gotMaxAge time.Duration) {
	t.Helper()
	if strings.TrimSpace(stub.lastReauthVerifier) == "" || strings.TrimSpace(stub.lastReauthNonce) == "" {
		t.Fatalf("expected the step-up start to mint a verifier and a nonce, got verifier=%q nonce=%q", stub.lastReauthVerifier, stub.lastReauthNonce)
	}
	if gotCode != wantCode {
		t.Fatalf("expected the exchange to use the callback code %q, got %q", wantCode, gotCode)
	}
	if gotVerifier != stub.lastReauthVerifier {
		t.Fatalf("expected the exchange to use the start's PKCE verifier %q, got %q", stub.lastReauthVerifier, gotVerifier)
	}
	if gotNonce != stub.lastReauthNonce {
		t.Fatalf("expected the exchange to check the start's nonce %q, got %q", stub.lastReauthNonce, gotNonce)
	}
	if gotMaxAge <= 0 {
		t.Fatalf("expected a positive max-age to bound the provider re-authentication, got %s", gotMaxAge)
	}
}

// assertReauthExchangeMatchesStart is the ValidateReauthExchange half
// (local-password setup, clear-data, account deletion).
func (stub *stubOIDCWorkflowService) assertReauthExchangeMatchesStart(t *testing.T, wantCode string) {
	t.Helper()
	assertStepupExchangeMatchesStart(t, stub, wantCode, stub.lastReauthCode, stub.lastReauthCodeVerifier, stub.lastReauthNonceCheck, stub.lastReauthMaxAge)
}

// assertIdentityLinkExchangeMatchesStart is the CompleteIdentityLinkReauth half.
func (stub *stubOIDCWorkflowService) assertIdentityLinkExchangeMatchesStart(t *testing.T, wantCode string) {
	t.Helper()
	assertStepupExchangeMatchesStart(t, stub, wantCode, stub.lastIdentityLinkCode, stub.lastIdentityLinkCodeVerifier, stub.lastIdentityLinkNonce, stub.lastIdentityLinkMaxAge)
}

// ListLinkedIdentities answers the stub's configured rows unchanged.
func (stub *stubOIDCWorkflowService) ListLinkedIdentities(_ context.Context, _ uint) ([]services.LinkedOIDCIdentity, error) {
	return stub.linkedIdentities, stub.listLinkedErr
}

// CompleteIdentityLinkReauth records the exchange and answers like
// ConfirmAndLinkIdentity's stand-in: it writes nothing, so the session version
// it reports is the one the step-up started from.
func (stub *stubOIDCWorkflowService) CompleteIdentityLinkReauth(_ context.Context, code string, codeVerifier string, expectedNonce string, targetUserID uint, expectedSessionVersion int, maxAuthAge time.Duration, _ time.Time) (int, error) {
	stub.lastIdentityLinkSessionVersion = expectedSessionVersion
	stub.lastIdentityLinkCode = code
	stub.lastIdentityLinkCodeVerifier = codeVerifier
	stub.lastIdentityLinkNonce = expectedNonce
	stub.lastIdentityLinkUserID = targetUserID
	stub.lastIdentityLinkMaxAge = maxAuthAge
	if stub.identityLinkReauthErr != nil {
		return 0, stub.identityLinkReauthErr
	}
	stub.lastConfirmLinkUserID = targetUserID
	stub.lastConfirmLinkClaims = stub.identityLinkClaims
	if stub.afterIdentityLinkConfirm != nil {
		stub.afterIdentityLinkConfirm()
	}
	if stub.confirmLinkErr != nil {
		return 0, stub.confirmLinkErr
	}
	return services.NormalizeAuthSessionVersion(expectedSessionVersion), nil
}

func TestLoginPageWithOIDCEnabledShowsSSOButton(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	rendered := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, rendered,
		// Structural hook only — the rendered SSO caption is Playwright's
		// subject (e2e/auth-oidc.spec.ts), sourced from the catalogue.
		bodyStringMatch{fragment: "data-auth-sso-cta", message: "expected SSO CTA marker in login page"},
	)
}

func TestOIDCStartRedirectSetsSealedStateCookie(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	assertStatusCode(t, response, http.StatusTemporaryRedirect)
	if location := response.Header.Get("Location"); location != stub.authURL {
		t.Fatalf("expected provider redirect %q, got %q", stub.authURL, location)
	}
	if stub.lastStartState == "" || stub.lastStartNonce == "" || stub.lastStartVerifier == "" {
		t.Fatal("expected OIDC start flow to generate state, nonce, and PKCE verifier")
	}
	assertOIDCDeadline(t, stub.lastStartDeadline)

	stateCookie := responseCookie(response.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected sealed OIDC state cookie")
	}
	if !stateCookie.HttpOnly {
		t.Fatal("expected OIDC state cookie HttpOnly=true")
	}
	if !stateCookie.Secure {
		t.Fatal("expected OIDC state cookie Secure=true")
	}
	if stateCookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("expected OIDC state cookie SameSite=None, got %v", stateCookie.SameSite)
	}
	if stateCookie.Path != security.OIDCCallbackPath {
		t.Fatalf("expected OIDC state cookie path %q, got %q", security.OIDCCallbackPath, stateCookie.Path)
	}
	if strings.Contains(stateCookie.Value, stub.lastStartState) || strings.Contains(stateCookie.Value, stub.lastStartNonce) {
		t.Fatalf("did not expect sealed OIDC state cookie to expose state or nonce in plaintext: %q", stateCookie.Value)
	}
}

func TestOIDCStartFailureClearsStateCookieAndFlashesLoginError(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.startErr = services.ErrOIDCUnavailable
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}
	assertOIDCDeadline(t, stub.lastStartDeadline)

	stateCookie := responseCookie(response.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie to be cleared on start failure")
	}
	if stateCookie.Value != "" {
		t.Fatalf("expected cleared OIDC state cookie, got %q", stateCookie.Value)
	}
	// /auth/oidc/start is an unguarded GET, no CSRF token possible on a safe
	// method: its own AuthError write goes through the exempt channel
	// (WEB-40), never the shared page slot.
	flashCookie := responseCookie(response.Cookies(), exemptFlashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected exempt-channel flash cookie on OIDC start failure")
	}
}

func TestOIDCCallbackSkipsCSRFAndFallsBackToStateValidation(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		enableCSRF:   true,
		oidcService:  newStubOIDCWorkflowService(true),
	})

	request := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {"missing"},
		"code":  {"provider-code"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}
	// POST /auth/oidc/callback is the sole CSRF exemption: its state-mismatch
	// refusal goes through the exempt channel (WEB-40), never the shared page
	// slot a pending same-origin flash occupies.
	if flashValue := responseCookieValue(response.Cookies(), exemptFlashCookieName); flashValue == "" {
		t.Fatal("expected exempt-channel flash cookie for invalid OIDC callback")
	}
}

func TestOIDCCallbackSuccessIssuesLocalAuthCookie(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.result = services.OIDCLoginResult{
		User: models.User{
			ID:                  11,
			Role:                models.RoleOwner,
			AuthSessionVersion:  1,
			OnboardingCompleted: true,
		},
		NewlyLinked: true,
	}
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	assertStatusCode(t, startResponse, http.StatusTemporaryRedirect)
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected owner redirect to /dashboard, got %q", location)
	}
	if stub.lastAuthCode != "provider-code" {
		t.Fatalf("expected callback code to reach OIDC service, got %q", stub.lastAuthCode)
	}
	if stub.lastAuthVerifier != stub.lastStartVerifier {
		t.Fatalf("expected callback to reuse PKCE verifier from state cookie, got %q", stub.lastAuthVerifier)
	}
	if stub.lastAuthExpectedNonce != stub.lastStartNonce {
		t.Fatalf("expected callback to reuse nonce from state cookie, got %q", stub.lastAuthExpectedNonce)
	}
	assertOIDCDeadline(t, stub.lastAuthDeadline)

	authCookie := responseCookie(callbackResponse.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected local auth cookie after successful OIDC callback")
	}
	if strings.Contains(authCookie.Value, "provider-code") {
		t.Fatalf("did not expect auth cookie to expose provider code: %q", authCookie.Value)
	}
	clearedStateCookie := responseCookie(callbackResponse.Cookies(), oidcStateCookieName)
	if clearedStateCookie == nil {
		t.Fatal("expected OIDC state cookie to be cleared after callback")
	}
	if clearedStateCookie.Value != "" {
		t.Fatalf("expected cleared OIDC state cookie, got %q", clearedStateCookie.Value)
	}
}

// The sign-in half of "a transit cookie is spent only for a callback that
// answers its own flow". The callback path is reachable by any site that can
// cause a navigation to it, so a request whose state does not match must leave
// the one-time cookie where it is — otherwise a stranger cancels a sign-in the
// owner is in the middle of, and the step-up half of the same rule
// (TestCrossSiteStepupCallbackRefusesAStateThatDoesNotMatchWithoutSpendingTheCookie)
// would be the only half anything holds to.
func TestOIDCCallbackMismatchingStateLeavesTheStateCookieForTheRealReturn(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.result = services.OIDCLoginResult{
		User: models.User{
			ID:                  12,
			Role:                models.RoleOwner,
			AuthSessionVersion:  1,
			OnboardingCompleted: true,
		},
	}
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	assertStatusCode(t, startResponse, http.StatusTemporaryRedirect)
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	postCallback := func(state string, code string) *http.Response {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
			"state": {state},
			"code":  {code},
		}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Cookie", stateCookie.String())
		return mustAppResponse(t, app, request)
	}

	stray := postCallback("not-the-sealed-state", "stranger-code")
	assertStatusCode(t, stray, http.StatusSeeOther)
	if location := stray.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected the refusal to land on /login, got %q", location)
	}
	if retracted := responseCookie(stray.Cookies(), oidcStateCookieName); retracted != nil && strings.TrimSpace(retracted.Value) == "" {
		t.Fatal("a mismatching callback must not expire the sign-in state cookie")
	}
	if stub.lastAuthCode != "" {
		t.Fatalf("a mismatching callback must not reach the token exchange, got code %q", stub.lastAuthCode)
	}

	// And the owner's real return trip still completes, which is what proves
	// the cookie above survived rather than merely not being re-sent.
	real := postCallback(stub.lastStartState, "provider-code")
	assertStatusCode(t, real, http.StatusSeeOther)
	if location := real.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected the real callback to sign in, got %q", location)
	}
	if authCookie := responseCookie(real.Cookies(), authCookieName); authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected the real callback to issue the session cookie")
	}
}

// TestSSOSignInStartDropsAnAbandonedStepup is the other half of "spend the
// step-up cookie only on a state match". Not spending it is what keeps a
// stranger from cancelling a step-up in progress — but the callback also
// DISPATCHES on that cookie's presence, so one the owner abandoned at the
// provider outranks the sign-in that comes next: the login state never
// matches it, the refusal returns before the sign-in branch is reached, and
// it flashes on the settings channel, which /login does not render. Every
// attempt fails silently for the cookie's whole ten minutes. Starting a
// sign-in therefore drops it, the mirror of what the three step-up starts
// already do to the login state cookie.
func TestSSOSignInStartDropsAnAbandonedStepup(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "abandoned-stepup-blocks-signin@example.com")
	fixture.oidcStub.authURL = "https://id.example.com/authorize"

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)

	// The owner leaves the provider without finishing, and later starts an
	// ordinary sign-in with that cookie still riding.
	request := httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil)
	request.Header.Set("Cookie", stepupCookie)
	signInStart := mustAppResponse(t, fixture.app, request)
	defer func() { _ = signInStart.Body.Close() }()
	assertStatusCode(t, signInStart, http.StatusTemporaryRedirect)

	retracted := responseCookie(signInStart.Cookies(), oidcStepupCookieName)
	if retracted == nil || strings.TrimSpace(retracted.Value) != "" {
		t.Fatal("expected the sign-in start to retract the abandoned step-up cookie")
	}
	stateCookie := responseCookie(signInStart.Cookies(), oidcStateCookieName)
	if stateCookie == nil || strings.TrimSpace(stateCookie.Value) == "" {
		t.Fatal("expected the sign-in start to mint a state cookie")
	}

	// What the browser has left is the state cookie alone, so the provider's
	// return reaches the sign-in branch instead of being answered by a
	// step-up that is no longer in flight.
	callback := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {fixture.oidcStub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callback.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callback.Header.Set("Cookie", cookiePair(stateCookie))
	completed := mustAppResponse(t, fixture.app, callback)
	defer func() { _ = completed.Body.Close() }()
	if location := completed.Header.Get("Location"); location == "/settings" {
		t.Fatal("the sign-in return was answered by the step-up branch: the abandoned cookie still decides")
	}
}

// TestSSOSignInStartDropsAnAbandonedStepupContinuation is the same rule for the
// step-up's other carrier. The hand-off a cross-site return parks seals the
// whole step-up plus an authorization code nobody has spent. Unlike the step-up
// cookie it cannot capture this callback — it is scoped to the continue route —
// but it can outlive the session that started it: a session that lapsed rather
// than being signed out never passed through clearSessionEndCookies, so without
// this the next sign-in leaves a restored tab everything it needs to finish the
// erasure the previous session abandoned.
func TestSSOSignInStartDropsAnAbandonedStepupContinuation(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "abandoned-continuation-outlives-session@example.com")
	fixture.oidcStub.reauthErr = nil
	fixture.oidcStub.authURL = "https://id.example.com/authorize"

	startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	bounce := crossSiteStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = bounce.Body.Close() }()
	continuation := continuationFromBounce(t, bounce)

	// The owner never follows the hand-off document. The tab sits there, the
	// session lapses, and the next thing the browser does is start a sign-in
	// with the continuation still riding.
	request := httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil)
	request.Header.Set("Cookie", cookiePair(continuation))
	signInStart := mustAppResponse(t, fixture.app, request)
	defer func() { _ = signInStart.Body.Close() }()
	assertStatusCode(t, signInStart, http.StatusTemporaryRedirect)

	retracted := responseCookie(signInStart.Cookies(), oidcStepupContinuationCookieName)
	if retracted == nil || strings.TrimSpace(retracted.Value) != "" {
		t.Fatal("expected the sign-in start to retract the abandoned step-up continuation")
	}
}

func TestOIDCCallbackProviderErrorRedirectsToLoginWithoutLeakingProviderError(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state":             {stub.lastStartState},
		"error":             {"access_denied"},
		"error_description": {"operator rejected sign-in"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}
	if strings.Contains(callbackResponse.Header.Get("Location"), "access_denied") {
		t.Fatal("did not expect provider error in callback redirect")
	}
	if stub.lastAuthCode != "" {
		t.Fatalf("did not expect OIDC authenticate call on provider error, got %q", stub.lastAuthCode)
	}

	flashCookie := responseCookie(callbackResponse.Cookies(), exemptFlashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected exempt-channel flash cookie on OIDC provider error")
	}
	if strings.Contains(flashCookie.Value, "access_denied") || strings.Contains(flashCookie.Value, "operator rejected sign-in") {
		t.Fatalf("did not expect provider error details in flash cookie: %q", flashCookie.Value)
	}
}

func TestOIDCCallbackAccountUnavailableRedirectsToLogin(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.authErr = services.ErrOIDCAccountUnavailable
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}
	assertOIDCDeadline(t, stub.lastAuthDeadline)

	if authCookie := responseCookie(callbackResponse.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect auth cookie on unavailable OIDC account")
	}
	flashCookie := responseCookie(callbackResponse.Cookies(), exemptFlashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected exempt-channel flash cookie on unavailable OIDC account")
	}
}

func TestOIDCCallbackResetRequiredRedirectsToResetPassword(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.result = services.OIDCLoginResult{
		User: models.User{
			ID:                 13,
			Role:               models.RoleOwner,
			AuthSessionVersion: 1,
			PasswordHash:       "$2a$10$0123456789abcdef01234uVwxyzABCD0123456789abcdef01234",
			MustChangePassword: true,
		},
		// The stub bypasses OIDCLoginService.Authenticate's own computation,
		// so RequiresPasswordReset is set here exactly as the real service
		// would derive it for a MustChangePassword account — the handler
		// branch under test consumes this field, not the raw
		// User.MustChangePassword.
		RequiresPasswordReset: true,
	}
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected redirect to /reset-password, got %q", location)
	}
	assertOIDCDeadline(t, stub.lastAuthDeadline)

	resetCookie := responseCookie(callbackResponse.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatal("expected reset-password cookie for forced OIDC reset")
	}
	if authCookie := responseCookie(callbackResponse.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect auth cookie on forced OIDC reset")
	}
}

// TestOIDCCallbackForLinkedTOTPAccountGatesOnTheSecondFactor pins session
// issuance parity (docs/security/oidc-and-sessions.md) between the OIDC login
// path and the local login path: an OIDC callback that resolves to an
// already-linked identity whose account has TOTP enabled must NOT mint an
// ovumcy_auth cookie directly off the exchange. It has to set the same
// pending-TOTP cookie the local login path sets (RequiresTOTP, mirroring
// LoginResult) and land on /auth/2fa; only completing that challenge may
// issue the session. Before this fix CompleteOIDCLogin fell straight through
// to setAuthCookie with no TOTP check anywhere on the path.
func TestOIDCCallbackForLinkedTOTPAccountGatesOnTheSecondFactor(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"

	secretKey := []byte(testHandlerSecretKey)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "oidc-totp@example.com", "StrongPass1", true)
	rawSecret := setupTOTPForUser(t, database, user.ID, secretKey)

	var linked models.User
	if err := database.First(&linked, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !linked.TOTPEnabled {
		t.Fatal("expected TOTP enabled on the account after setup")
	}

	// The stub bypasses OIDCLoginService.Authenticate's own computation, so the
	// result carries RequiresTOTP exactly as the real service would derive it
	// for this account (TOTP enabled, MustChangePassword false) — the handler
	// gate under test is what consumes this field, not what computes it. Logout
	// is also populated, as buildLogoutState would for a provider with
	// end-session support, so the callback has to stage it under an opaque id
	// rather than discard it — the parity this test exists to pin.
	stub.result = services.OIDCLoginResult{
		User:         linked,
		RequiresTOTP: true,
		Logout: &services.OIDCLogoutState{
			UserID:                linked.ID,
			EndSessionEndpoint:    testOIDCIssuerURL + "/logout",
			IDTokenHint:           "eyJhbGciOiJSUzI1NiJ9.header.signature",
			PostLogoutRedirectURL: "https://app.example.com/",
		},
	}

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/auth/2fa" {
		t.Fatalf("expected redirect to /auth/2fa, got %q", location)
	}
	if authCookie := responseCookie(callbackResponse.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect an auth cookie before the TOTP challenge is completed")
	}
	pendingCookie := responseCookie(callbackResponse.Cookies(), totpPendingCookieName)
	if pendingCookie == nil || strings.TrimSpace(pendingCookie.Value) == "" {
		t.Fatal("expected a TOTP pending cookie from the OIDC callback")
	}

	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	decoded, err := codec.open(totpPendingCookieName, pendingCookie.Value)
	if err != nil {
		t.Fatalf("open pending cookie: %v", err)
	}
	var pendingPayload totpPendingCookiePayload
	if err := json.Unmarshal(decoded, &pendingPayload); err != nil {
		t.Fatalf("unmarshal pending payload: %v", err)
	}
	if pendingPayload.UserID != linked.ID {
		t.Fatalf("pending cookie user_id = %d, want %d", pendingPayload.UserID, linked.ID)
	}
	pendingLogoutStateID := strings.TrimSpace(pendingPayload.OIDCLogoutStateID)
	if pendingLogoutStateID == "" {
		t.Fatal("expected the pending cookie to carry an opaque OIDC logout-state id, since the stubbed result carries provider-logout material")
	}

	logoutStateSvc := services.NewOIDCLogoutStateService(db.NewRepositories(database).OIDCLogout)
	stagedState, stagedFound, err := logoutStateSvc.Load(context.Background(), pendingLogoutStateID, linked.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("load staged logout state: %v", err)
	}
	if !stagedFound || stagedState.EndSessionEndpoint != stub.result.Logout.EndSessionEndpoint {
		t.Fatalf("expected the callback to stage the provider-logout material under the opaque id, got found=%v state=%#v", stagedFound, stagedState)
	}

	// Completing the challenge with a valid code is what may issue the
	// session — never the callback itself.
	code, err := totp.GenerateCode(rawSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	challengeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge", strings.NewReader(url.Values{
		"code": {code},
	}.Encode()))
	challengeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	challengeRequest.Header.Set("Cookie", totpPendingCookieName+"="+pendingCookie.Value)

	challengeResponse := mustAppResponse(t, app, challengeRequest)
	assertStatusCode(t, challengeResponse, http.StatusSeeOther)
	sessionCookie := responseCookie(challengeResponse.Cookies(), authCookieName)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		t.Fatal("expected an auth cookie after completing the TOTP challenge")
	}

	// The provider-logout material must have followed the session onto its
	// real id — the whole point of staging it under the opaque id above — and
	// the staging row itself must be gone rather than left behind as a second,
	// orphaned copy.
	newSessionID := mustExtractAuthSessionIDFromCookieHeader(t, sessionCookie.Name+"="+sessionCookie.Value)
	movedState, movedFound, err := logoutStateSvc.Load(context.Background(), newSessionID, linked.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("load relocated logout state: %v", err)
	}
	if !movedFound || movedState.EndSessionEndpoint != stub.result.Logout.EndSessionEndpoint {
		t.Fatalf("expected the TOTP challenge to relocate the logout state onto the new session id, got found=%v state=%#v", movedFound, movedState)
	}
	_, stillStaged, err := logoutStateSvc.Load(context.Background(), pendingLogoutStateID, linked.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("load staging row after relocation: %v", err)
	}
	if stillStaged {
		t.Fatal("expected the opaque staging row to be deleted once its state moved to the real session id")
	}
}

// TestOIDCCallbackForLinkedTOTPAccountWithNoLogoutStateCompletesChallengeCleanly
// covers the other half of the same parity: when the OIDC result carries no
// provider-logout material at all (Logout == nil — no end_session_endpoint,
// or provider logout disabled), the pending cookie carries no logout-state id,
// completing the challenge mints the session with no OIDC logout row attached
// to it, and the bridge cookie is cleared exactly as the direct, non-gated
// OIDC success path clears it.
func TestOIDCCallbackForLinkedTOTPAccountWithNoLogoutStateCompletesChallengeCleanly(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"

	secretKey := []byte(testHandlerSecretKey)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "oidc-totp-no-logout@example.com", "StrongPass1", true)
	rawSecret := setupTOTPForUser(t, database, user.ID, secretKey)

	var linked models.User
	if err := database.First(&linked, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}

	stub.result = services.OIDCLoginResult{
		User:         linked,
		RequiresTOTP: true,
		Logout:       nil,
	}

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}
	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())
	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)

	pendingCookie := responseCookie(callbackResponse.Cookies(), totpPendingCookieName)
	if pendingCookie == nil || strings.TrimSpace(pendingCookie.Value) == "" {
		t.Fatal("expected a TOTP pending cookie from the OIDC callback")
	}
	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	decoded, err := codec.open(totpPendingCookieName, pendingCookie.Value)
	if err != nil {
		t.Fatalf("open pending cookie: %v", err)
	}
	var pendingPayload totpPendingCookiePayload
	if err := json.Unmarshal(decoded, &pendingPayload); err != nil {
		t.Fatalf("unmarshal pending payload: %v", err)
	}
	if strings.TrimSpace(pendingPayload.OIDCLogoutStateID) != "" {
		t.Fatalf("expected no OIDC logout-state id when the result carries no logout material, got %q", pendingPayload.OIDCLogoutStateID)
	}

	code, err := totp.GenerateCode(rawSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	challengeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge", strings.NewReader(url.Values{
		"code": {code},
	}.Encode()))
	challengeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	challengeRequest.Header.Set("Cookie", totpPendingCookieName+"="+pendingCookie.Value)
	challengeResponse := mustAppResponse(t, app, challengeRequest)
	assertStatusCode(t, challengeResponse, http.StatusSeeOther)

	sessionCookie := responseCookie(challengeResponse.Cookies(), authCookieName)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		t.Fatal("expected an auth cookie after completing the TOTP challenge")
	}
	newSessionID := mustExtractAuthSessionIDFromCookieHeader(t, sessionCookie.Name+"="+sessionCookie.Value)

	logoutStateSvc := services.NewOIDCLogoutStateService(db.NewRepositories(database).OIDCLogout)
	_, found, err := logoutStateSvc.Load(context.Background(), newSessionID, linked.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("load logout state for new session: %v", err)
	}
	if found {
		t.Fatal("expected no OIDC logout state attached to a session minted with no logout material to carry")
	}

	bridgeCookie := responseCookie(challengeResponse.Cookies(), oidcLogoutBridgeCookieName)
	if bridgeCookie == nil || strings.TrimSpace(bridgeCookie.Value) != "" {
		t.Fatal("expected the TOTP challenge to clear the OIDC logout bridge cookie, same as every other session-mint path")
	}
}

// testOIDCIssuerURL is the issuer the stub and the default test wiring report.
// Stored provider-logout state is pinned to the issuer origin, so a fixture
// whose end-session endpoint must survive that pin sits on this origin.
const testOIDCIssuerURL = "https://id.example.com"

// testOIDCPostLogoutRedirectURL is the post-logout return address the stub and
// the default test wiring resolve. The provider redirect is composed from it,
// never from the address stored with the logout state.
const testOIDCPostLogoutRedirectURL = "https://ovumcy.example.com/login"

func newStubOIDCWorkflowService(enabled bool) *stubOIDCWorkflowService {
	return &stubOIDCWorkflowService{
		enabled:                enabled,
		localPublicAuthEnabled: true,
		issuerURL:              testOIDCIssuerURL,
		postLogoutRedirectURL:  testOIDCPostLogoutRedirectURL,
	}
}

func assertOIDCDeadline(t *testing.T, deadline time.Time) {
	t.Helper()

	if deadline.IsZero() {
		t.Fatal("expected bounded OIDC context deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 5*time.Second || remaining > 15*time.Second {
		t.Fatalf("expected OIDC deadline near %s, got remaining %s", oidcExternalRequestTimeout, remaining)
	}
}

// TestOIDCCallbackPersistsProviderLogoutStateOnSuccessfulLogin drives a full
// form_post OIDC login whose Authenticate result carries a provider logout state
// (result.Logout != nil), so CompleteOIDCLogin takes the L122 Save arm:
//
//	if err := handler.oidcLogoutStateSvc.Save(...); err != nil { redirect /login }
//
// The Save runs against the real test store and SUCCEEDS (err == nil), so on the
// original code the handler proceeds to issue the session and redirect to the
// owner's post-login path. The CONDITIONALS_NEGATION mutant flips the guard to
// `err == nil`, which treats a SUCCESSFUL save as a failure: it logs an error,
// clears the auth cookies, and redirects to /login — breaking every OIDC login
// that also sets up a provider-logout bridge. That branch is otherwise only
// exercised by the e2e OIDC lanes (skipped in default CI), so absent this test
// the mutant survives the unit suite.
//
// Asserting the authenticated redirect + a live auth cookie + the persisted
// logout state pins the success semantics and fails red under the mutation.
func TestOIDCCallbackPersistsProviderLogoutStateOnSuccessfulLogin(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	result := newOwnerOIDCLoginResult()
	result.Logout = &services.OIDCLogoutState{
		UserID:                result.User.ID,
		EndSessionEndpoint:    "https://id.example.com/logout",
		IDTokenHint:           "eyJhbGciOiJSUzI1NiJ9.header.signature",
		PostLogoutRedirectURL: "https://app.example.com/login",
	}
	stub.result = result

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)

	wantLocation := services.PostLoginRedirectPath(&result.User)
	if location := callbackResponse.Header.Get("Location"); location != wantLocation {
		t.Fatalf("successful OIDC login with a provider logout state must land authenticated at %q, got %q", wantLocation, location)
	}

	authCookie := responseCookie(callbackResponse.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected a live auth cookie after a successful OIDC login; the logout-state save must not clear the session")
	}

	// The Save arm ran and succeeded: the provider logout state is now keyed on
	// the freshly issued session id. Loading it back proves we were on the
	// err == nil path the mutant inverts.
	newSessionID := mustExtractAuthSessionIDFromCookieHeader(t, authCookie.Name+"="+authCookie.Value)
	stateService := services.NewOIDCLogoutStateService(db.NewRepositories(database).OIDCLogout)
	saved, found, err := stateService.Load(context.Background(), newSessionID, result.User.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("load persisted logout state: %v", err)
	}
	if !found {
		t.Fatal("expected the provider logout state to be persisted on the new session id after a successful login")
	}
	if saved.EndSessionEndpoint != result.Logout.EndSessionEndpoint {
		t.Fatalf("persisted end_session_endpoint = %q, want %q", saved.EndSessionEndpoint, result.Logout.EndSessionEndpoint)
	}
}

const testHandlerSecretKey = "test-secret-key"

func decodeFlashCookieForTest(t *testing.T, sealed string) FlashPayload {
	t.Helper()
	codec, err := newSecureCookieCodec([]byte(testHandlerSecretKey))
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	decoded, err := codec.open(flashCookieName, sealed)
	if err != nil {
		t.Fatalf("open flash cookie: %v", err)
	}
	payload := FlashPayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("unmarshal flash payload: %v", err)
	}
	return payload
}

// decodeExemptFlashCookieForTest is decodeFlashCookieForTest's twin for the
// WEB-40 exempt channel: the cookie name is bound into the sealed envelope, so
// a value sealed under exemptFlashCookieName does not open under
// flashCookieName.
func decodeExemptFlashCookieForTest(t *testing.T, sealed string) FlashPayload {
	t.Helper()
	codec, err := newSecureCookieCodec([]byte(testHandlerSecretKey))
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	decoded, err := codec.open(exemptFlashCookieName, sealed)
	if err != nil {
		t.Fatalf("open exempt flash cookie: %v", err)
	}
	payload := FlashPayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("unmarshal exempt flash payload: %v", err)
	}
	return payload
}

// TestOIDCCallbackPendingLinkNeverMintsPendingCookieAndRedirectsToLogin pins
// the fail-closed handoff WEB-77 left in place after removing the public
// link-confirm route for good (issue #701 had already made it unreachable):
// when service.Authenticate returns ErrOIDCLinkRequiresConfirmation for a
// target local user (including one with a usable local password — the case
// the old password-confirmation page used to handle), the callback must NOT
// seal a link-pending cookie and must redirect straight to /login. The only
// ways to complete this link are the authenticated Settings step-up and the
// operator CLI.
func TestOIDCCallbackPendingLinkNeverMintsPendingCookieAndRedirectsToLogin(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.result = services.OIDCLoginResult{
		User: models.User{
			ID:                 21,
			Role:               models.RoleOwner,
			AuthSessionVersion: 1,
			LocalAuthEnabled:   true,
			Email:              "owner@example.com",
		},
		PendingLinkClaims: &security.OIDCClaims{
			Issuer:  "https://idp.example",
			Subject: "subject-42",
			Email:   "owner@example.com",
		},
	}
	stub.authErr = services.ErrOIDCLinkRequiresConfirmation
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	response := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login (link-confirm route removed for good), got %q", location)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("did not expect auth cookie to be issued, got %q", authCookie.Value)
	}
	flashCookie := responseCookie(response.Cookies(), exemptFlashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected exempt-channel flash cookie explaining the refusal")
	}
	payload := decodeExemptFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != authOIDCLinkConfirmUnavailableErrorSpec().Key {
		t.Fatalf("expected flash auth_error %q, got %q", authOIDCLinkConfirmUnavailableErrorSpec().Key, payload.AuthError)
	}
	// The refusal must mint no pending-link cookie — the property #701 already
	// established and this handoff must not regress: the response sets nothing
	// beyond the flash and the state cookie's clear (never the sealed
	// "ovumcy_oidc_link_pending" cookie the retired link-confirm handler read).
	if pending := responseCookie(response.Cookies(), "ovumcy_oidc_link_pending"); pending != nil {
		t.Fatalf("expected no ovumcy_oidc_link_pending cookie, got %q", pending.Value)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name != exemptFlashCookieName && cookie.Name != oidcStateCookieName {
			t.Fatalf("expected only the exempt flash cookie and the state cookie's clear, got unexpected cookie %q", cookie.Name)
		}
	}
}

// TestOIDCLinkConfirmRouteIsRemoved is WEB-77's direct pin on the removal
// itself, distinct from the handoff test above: issue #701 had already made
// the route unreachable (no pending-link cookie was ever minted for it), but
// GET/POST /auth/oidc/link-confirm still matched a registered route and ran
// CSRF + the handler's own gates before ever finding that out. Neither method
// may match a route any longer — both must fall through to the ordinary 404.
func TestOIDCLinkConfirmRouteIsRemoved(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})

	getResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/link-confirm", nil))
	assertStatusCode(t, getResponse, http.StatusNotFound)

	postRequest := httptest.NewRequest(http.MethodPost, "/auth/oidc/link-confirm", strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postResponse := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, postResponse, http.StatusNotFound)
}

// TestMapAuthOIDCError locks the OIDCService-failure -> APIErrorSpec contract
// consumed by the sign-in/callback handlers: sentinel classes collapse onto a
// small set of enumeration-safe specs (unavailable vs authentication failed vs
// account unavailable) so provider/auth failures never leak account state
// through error granularity, and every unmapped error falls back to the
// generic authentication-failed response.
func TestMapAuthOIDCError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{name: "disabled maps to unavailable", err: services.ErrOIDCDisabled, want: authOIDCUnavailableErrorSpec()},
		{name: "unavailable maps to unavailable", err: services.ErrOIDCUnavailable, want: authOIDCUnavailableErrorSpec()},
		{name: "callback invalid maps to authentication failed", err: services.ErrOIDCCallbackInvalid, want: authOIDCAuthenticationFailedErrorSpec()},
		{name: "authentication failed maps to authentication failed", err: services.ErrOIDCAuthenticationFailed, want: authOIDCAuthenticationFailedErrorSpec()},
		{name: "account unavailable maps to account unavailable", err: services.ErrOIDCAccountUnavailable, want: authOIDCAccountUnavailableErrorSpec()},
		{name: "identity resolve failed maps to unavailable", err: services.ErrOIDCIdentityResolveFailed, want: authOIDCUnavailableErrorSpec()},
		{name: "link failed maps to unavailable", err: services.ErrOIDCLinkFailed, want: authOIDCUnavailableErrorSpec()},
		{name: "provision failed maps to unavailable", err: services.ErrOIDCProvisionFailed, want: authOIDCUnavailableErrorSpec()},
		{name: "unknown falls back to authentication failed", err: errors.New("unmapped oidc error"), want: authOIDCAuthenticationFailedErrorSpec()},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mapAuthOIDCError(tt.err); got != tt.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, tt.want)
			}
		})
	}
}
