package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
)

type stubOIDCWorkflowService struct {
	enabled                bool
	localPublicAuthEnabled bool
	responseMode           security.OIDCResponseMode
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

func (stub *stubOIDCWorkflowService) ConfirmAndLinkIdentity(ctx context.Context, targetUserID uint, claims security.OIDCClaims, _ time.Time) error {
	stub.lastConfirmLinkUserID = targetUserID
	stub.lastConfirmLinkClaims = claims
	return stub.confirmLinkErr
}

func (stub *stubOIDCWorkflowService) CompleteIdentityLinkReauth(_ context.Context, code string, codeVerifier string, expectedNonce string, targetUserID uint, maxAuthAge time.Duration, _ time.Time) error {
	stub.lastIdentityLinkCode = code
	stub.lastIdentityLinkCodeVerifier = codeVerifier
	stub.lastIdentityLinkNonce = expectedNonce
	stub.lastIdentityLinkUserID = targetUserID
	stub.lastIdentityLinkMaxAge = maxAuthAge
	if stub.identityLinkReauthErr != nil {
		return stub.identityLinkReauthErr
	}
	stub.lastConfirmLinkUserID = targetUserID
	stub.lastConfirmLinkClaims = stub.identityLinkClaims
	return stub.confirmLinkErr
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
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected flash cookie on OIDC start failure")
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
	if flashValue := responseCookieValue(response.Cookies(), flashCookieName); flashValue == "" {
		t.Fatal("expected flash cookie for invalid OIDC callback")
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

	flashCookie := responseCookie(callbackResponse.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected flash cookie on OIDC provider error")
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
	flashCookie := responseCookie(callbackResponse.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected flash cookie on unavailable OIDC account")
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
			EndSessionEndpoint:    "https://idp.example.com/logout",
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

func newStubOIDCWorkflowService(enabled bool) *stubOIDCWorkflowService {
	return &stubOIDCWorkflowService{
		enabled:                enabled,
		localPublicAuthEnabled: true,
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

// OIDC link-confirm handler regressions. These exercise the password-gated
// first-time-link flow added in commit d1def85 (security(auth/oidc): gate
// first-time link to existing email behind password confirmation). The
// link-pending cookie itself is covered for AAD/tamper/rotation in
// oidc_link_pending_cookie_test.go; these tests assert the routes that
// consume it: startOIDCLinkConfirmation (dispatched from CompleteOIDCLogin),
// ShowOIDCLinkConfirmPage (GET), CompleteOIDCLinkConfirmation (POST), and
// the error mapper that translates ConfirmAndLinkIdentity failures.

const testHandlerSecretKey = "test-secret-key"

func sealLinkPendingCookieForTest(t *testing.T, payload oidcLinkPendingPayload) string {
	t.Helper()
	codec, err := newSecureCookieCodec([]byte(testHandlerSecretKey))
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal link-pending payload: %v", err)
	}
	sealed, err := codec.seal(oidcLinkPendingCookieName, serialized)
	if err != nil {
		t.Fatalf("seal link-pending cookie: %v", err)
	}
	return sealed
}

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

// TestOIDCCallbackPendingLinkNeverMintsPendingCookieAndRedirectsToLogin pins
// issue #701's closure of the public link-confirm route: when
// service.Authenticate returns ErrOIDCLinkRequiresConfirmation for a target
// local user (including one with a usable local password — the case the old
// password-confirmation page used to handle), the callback must NOT seal a
// link-pending cookie and must NOT hand off to /auth/oidc/link-confirm. It
// redirects straight to /login; the only ways to complete this link now are
// the authenticated Settings step-up and the operator CLI.
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
		t.Fatalf("expected redirect to /login (public link-confirm stays closed), got %q", location)
	}
	if linkCookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName); linkCookie != nil && strings.TrimSpace(linkCookie.Value) != "" {
		t.Fatalf("did not expect a link-pending cookie to ever be minted, got %q", linkCookie.Value)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("did not expect auth cookie to be issued, got %q", authCookie.Value)
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected flash cookie explaining the refusal")
	}
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != authOIDCLinkConfirmUnavailableErrorSpec().Key {
		t.Fatalf("expected flash auth_error %q, got %q", authOIDCLinkConfirmUnavailableErrorSpec().Key, payload.AuthError)
	}
}

// TestPublicOIDCLinkConfirmRouteNeverCompletesALinkWithoutAMintedCookie is the
// direct pin that the public route is closed: with no way left to obtain a
// genuine sealed link-pending cookie (startOIDCLinkConfirmation never mints
// one anymore), posting a password straight at /auth/oidc/link-confirm — the
// exact request the old self-service flow accepted — cannot complete a link,
// because the handler's very first check is for that cookie.
func TestPublicOIDCLinkConfirmRouteNeverCompletesALinkWithoutAMintedCookie(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	createOnboardingTestUser(t, database, "still-closed@example.com", "StrongPass1", true)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login (no pending-link cookie), got %q", location)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("expected no auth cookie from the closed public route, got %q", authCookie.Value)
	}
	if stub.lastConfirmLinkUserID != 0 {
		t.Fatalf("expected ConfirmAndLinkIdentity to never run, but it ran for user id %d", stub.lastConfirmLinkUserID)
	}
}

func TestShowOIDCLinkConfirmPageWithoutCookieRedirectsToLogin(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, oidcLinkConfirmPath, nil))
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login when no link-pending cookie, got %q", location)
	}
}

func TestShowOIDCLinkConfirmPageWithSealedCookieRendersForm(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})

	payload, err := newOIDCLinkPendingPayload(time.Now().UTC(), 41, "https://idp.example", "subject-form", "owner-form@example.com")
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, payload)

	request := httptest.NewRequest(http.MethodGet, oidcLinkConfirmPath, nil)
	request.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	if !strings.Contains(body, "owner-form@example.com") {
		t.Fatalf("expected rendered confirm page to expose target email, got body without it")
	}
}

func TestCompleteOIDCLinkConfirmationWithoutCookieRedirectsToLoginWithExpiredKey(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"anything"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login without link-pending cookie, got %q", location)
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected flash cookie with expiration error")
	}
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != authOIDCLinkConfirmExpiredErrorSpec().Key {
		t.Fatalf("expected flash auth_error %q, got %q", authOIDCLinkConfirmExpiredErrorSpec().Key, payload.AuthError)
	}
}

// TestCompleteOIDCLinkConfirmationKeepsCookieOnWrongPassword locks the
// retry-within-TTL behavior: a single wrong-password attempt must keep the
// sealed cookie so the user can retry inside the 5-minute window. Clearing
// after the first wrong attempt would prevent the rate-limited retry the
// per-IP /auth/oidc/* limiter is sized for.
func TestCompleteOIDCLinkConfirmationKeepsCookieOnWrongPassword(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})
	user := createOnboardingTestUser(t, database, "link-wrong@example.com", "StrongPass1", true)

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-wrong", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"WrongPass2"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != oidcLinkConfirmPath {
		t.Fatalf("expected redirect back to link-confirm on wrong password, got %q", location)
	}
	if cleared := responseCookie(response.Cookies(), oidcLinkPendingCookieName); cleared != nil && cleared.Value == "" {
		t.Fatal("expected link-pending cookie to remain sealed after wrong password (retry-within-TTL)")
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect auth cookie on wrong-password attempt")
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected flash cookie with invalid-password error")
	}
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != authOIDCLinkConfirmInvalidPasswordErrorSpec().Key {
		t.Fatalf("expected flash auth_error %q, got %q", authOIDCLinkConfirmInvalidPasswordErrorSpec().Key, payload.AuthError)
	}
}

// TestShowOIDCLinkConfirmPageRendersLocalizedFlashError closes the render half of
// the wrong-password path above: the existing tests stop at the flash payload,
// which carries the error SPEC key, and the page used to hand that key straight
// to the template. Since the template translates ErrorKey and an unknown key
// comes back unchanged, the page displayed the raw English "sso link confirmation
// invalid password" to every language while the localized entries sat unused.
func TestShowOIDCLinkConfirmPageRendersLocalizedFlashError(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})
	user := createOnboardingTestUser(t, database, "link-flash-render@example.com", "StrongPass1", true)

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-flash-render", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	pendingCookie := oidcLinkPendingCookieName + "=" + sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"WrongPass2"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", pendingCookie)

	postResponse := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, postResponse, http.StatusSeeOther)
	flashCookie := responseCookie(postResponse.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected flash cookie with invalid-password error")
	}

	getRequest := httptest.NewRequest(http.MethodGet, oidcLinkConfirmPath, nil)
	getRequest.Header.Set("Accept-Language", "en")
	getRequest.Header.Set("Cookie", joinCookieHeader(pendingCookie, cookiePair(flashCookie)))

	getResponse := mustAppResponse(t, app, getRequest)
	assertStatusCode(t, getResponse, http.StatusOK)
	body := mustReadBodyString(t, getResponse.Body)

	specKey := authOIDCLinkConfirmInvalidPasswordErrorSpec().Key
	localeKey := services.AuthErrorTranslationKey(specKey)
	if localeKey == "" {
		t.Fatalf("error spec %q has no locale mapping — the page cannot localize it", specKey)
	}
	errorBlock := htmlAuthErrorByKey(mustParseHTMLDocument(t, body), localeKey)
	if errorBlock == nil {
		t.Fatalf("expected the link-confirm page to report the error under the locale key %s after a wrong password", localeKey)
	}
	if message := normalizeHTMLText(htmlNodeText(errorBlock)); strings.Contains(message, specKey) {
		t.Fatalf("link-confirm page rendered the raw error spec key as its message: %q", message)
	}
}

func TestCompleteOIDCLinkConfirmationWithEmptyPasswordFlashesInvalidPassword(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})
	user := createOnboardingTestUser(t, database, "link-empty@example.com", "StrongPass1", true)

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-empty", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"   "},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != oidcLinkConfirmPath {
		t.Fatalf("expected redirect back to link-confirm on empty password, got %q", location)
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil {
		t.Fatal("expected flash cookie on empty password")
	}
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != authOIDCLinkConfirmInvalidPasswordErrorSpec().Key {
		t.Fatalf("expected flash auth_error %q, got %q", authOIDCLinkConfirmInvalidPasswordErrorSpec().Key, payload.AuthError)
	}
}

func TestCompleteOIDCLinkConfirmationWithCorrectPasswordLinksAndIssuesAuthCookie(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "link-ok@example.com", "StrongPass1", true)

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-ok", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected owner redirect to /dashboard after successful link, got %q", location)
	}
	if stub.lastConfirmLinkUserID != user.ID {
		t.Fatalf("expected ConfirmAndLinkIdentity to receive user id %d, got %d", user.ID, stub.lastConfirmLinkUserID)
	}
	if stub.lastConfirmLinkClaims.Issuer != "https://idp.example" || stub.lastConfirmLinkClaims.Subject != "subject-ok" {
		t.Fatalf("expected ConfirmAndLinkIdentity to receive sealed claims, got %+v", stub.lastConfirmLinkClaims)
	}
	authCookie := responseCookie(response.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected auth cookie after successful password challenge")
	}
	clearedLinkCookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName)
	if clearedLinkCookie == nil {
		t.Fatal("expected link-pending cookie to be cleared on success")
	}
	if clearedLinkCookie.Value != "" {
		t.Fatalf("expected link-pending cookie to be cleared, got %q", clearedLinkCookie.Value)
	}
}

// TestCompleteOIDCLinkConfirmationRoutesMustChangePasswordToReset locks that
// when the target user has MustChangePassword set, the link-confirm path
// must hand off to /reset-password with a reset-password cookie and must
// NOT issue a regular auth cookie. Otherwise a forced-rotation user could
// skip the rotation by linking an OIDC identity.
func TestCompleteOIDCLinkConfirmationRoutesMustChangePasswordToReset(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "link-reset@example.com", "StrongPass1", true)
	if err := database.Model(&user).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("set must_change_password: %v", err)
	}

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-reset", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected redirect to /reset-password for forced rotation, got %q", location)
	}
	resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatal("expected reset-password cookie for forced rotation path")
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect auth cookie on forced-rotation link path")
	}
}

// TestCompleteOIDCLinkConfirmationRoutesAForcedResetTOTPTargetToResetWithoutASession
// is the mirror of the test above for a target carrying BOTH
// must_change_password and totp_enabled, and it pins a DECISION rather than
// merely the behaviour that happens to exist today: the forced reset outranks
// the TOTP challenge, so link-confirm must land on /reset-password with a
// reset-password cookie and must NOT issue an ovumcy_auth session.
//
// The ordering is deliberate, and it is the intentional recovery path for an
// owner whose second factor is unusable. TOTP secrets are encrypted under a key
// derived from the application secret, so after a SECRET_KEY rotation the
// stored ciphertext no longer opens and no code the authenticator produces can
// ever satisfy the challenge; the operator-forced reset
// (`ovumcy reset-password <email>`) is the way back in that
// docs/security/cryptography.md and docs/self-hosted.md instruct an operator to
// use. Making the second factor win would withdraw that escape hatch from
// precisely the accounts whose second factor is already broken. It is not a
// downgrade of session security: the reset still bumps AuthSessionVersion in
// the same atomic update that writes the new password hash
// (internal/db/user_repository.go).
//
// This handler is the sharp end of that decision, and the reason the case has
// to exist HERE and not only at the login route or in the service. It branches
// on the LoginService result's RequiresPasswordReset and has no RequiresTOTP
// branch at all — its own second-factor gate reads targetUser.TOTPEnabled and
// runs earlier. So were the service's precedence reversed, this target would
// come back with RequiresPasswordReset == false, the reset branch would be
// skipped, and control would fall through to setAuthCookie: a forced-rotation
// account handed a live session by the link-confirm route while the login route
// still refuses it. The sibling above cannot see that, because its fixture
// never enables TOTP. The submission below therefore carries a VALID totp_code
// — the handler's own gate must be satisfied for the fall-through to be
// reachable at all, and an assertion that stopped at the gate would prove
// nothing about the precedence.
//
// Read a failure here as "the decision was reversed", not as a stale assertion.
// The public declaration of this accepted risk is recorded separately from this
// test.
func TestCompleteOIDCLinkConfirmationRoutesAForcedResetTOTPTargetToResetWithoutASession(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "link-reset-totp@example.com", "StrongPass1", true)
	rawSecret := setupTOTPForUser(t, database, user.ID, []byte(testHandlerSecretKey))
	if err := database.Model(&user).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("set must_change_password: %v", err)
	}

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-reset-totp", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	code, err := totp.GenerateCode(rawSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password":  {"StrongPass1"},
		"totp_code": {code},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected redirect to /reset-password for a forced-rotation target with TOTP enabled, got %q", location)
	}
	resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatal("expected reset-password cookie on the forced-reset recovery path")
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect an auth cookie for a forced-rotation target with TOTP enabled")
	}
}

func TestCompleteOIDCLinkConfirmationWithLocalAuthDisabledRefusesUnavailable(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})
	user := createOnboardingTestUser(t, database, "link-disabled@example.com", "StrongPass1", true)
	if err := database.Model(&user).Update("local_auth_enabled", false).Error; err != nil {
		t.Fatalf("disable local auth: %v", err)
	}

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-disabled", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login when local auth disabled mid-flow, got %q", location)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect auth cookie when local auth disabled mid-flow")
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil {
		t.Fatal("expected flash cookie explaining the refusal")
	}
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != authOIDCLinkConfirmUnavailableErrorSpec().Key {
		t.Fatalf("expected flash auth_error %q, got %q", authOIDCLinkConfirmUnavailableErrorSpec().Key, payload.AuthError)
	}
}

// TestCompleteOIDCLinkConfirmationRefusesEntirelyWhenLocalSignInDisabled locks
// F2: on an oidc-only instance (the operator turned the instance-level local
// sign-in toggle off), a correct password must not be accepted at all — the
// same refusal Login gives in that state (authLocalSignInDisabledErrorSpec,
// /login redirect, no auth cookie), checked at the same point in the handler
// Login checks it (first, before anything else runs).
//
// Critically, ConfirmAndLinkIdentity must NOT run either. Gating only the
// session mint and leaving the identity link itself open would not close F2:
// the link is a PERMANENT (issuer, subject) -> account binding, and the
// password that authorizes it is exactly the credential the operator
// disabled — the realistic reason to disable it being that it leaked. An
// attacker holding that leaked password could drive the OIDC callback with an
// IdP identity carrying the victim's email, hit
// ErrOIDCLinkRequiresConfirmation, post the password here, and — if the link
// were allowed through — sign in through the newly linked identity on the
// very next OIDC round-trip with no password prompt at all. See
// TestOIDCLinkConfirmRefusalWhileDisabledLeavesNoPersistentBinding for that
// second leg pinned end-to-end.
func TestCompleteOIDCLinkConfirmationRefusesEntirelyWhenLocalSignInDisabled(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.localPublicAuthEnabled = false
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "link-oidc-only@example.com", "StrongPass1", true)

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-oidc-only", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login when local sign-in is disabled, got %q", location)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect an auth cookie when local sign-in is disabled instance-wide")
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil {
		t.Fatal("expected flash cookie explaining the refusal")
	}
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	// Exactly Login's refusal spec, not a bespoke one: same helper, same key.
	wantSpec := authLocalSignInDisabledErrorSpec()
	if wantSpec.Status != fiber.StatusForbidden {
		t.Fatalf("test fixture assumption broken: authLocalSignInDisabledErrorSpec status changed to %d", wantSpec.Status)
	}
	if payload.AuthError != wantSpec.Key {
		t.Fatalf("expected flash auth_error %q (Login's own local-sign-in-disabled key), got %q", wantSpec.Key, payload.AuthError)
	}

	// The identity link must NOT have gone through: authorizing a permanent
	// binding with the disabled password is the actual defect, not merely
	// issuing a session with it.
	if stub.lastConfirmLinkUserID != 0 {
		t.Fatalf("expected ConfirmAndLinkIdentity NOT to run while local sign-in is disabled, but it ran for user id %d", stub.lastConfirmLinkUserID)
	}
}

// TestCompleteOIDCLinkConfirmationConfirmLinkErrorMappingClearsCookie locks
// that when ConfirmAndLinkIdentity fails (provider/storage errors), the
// pending cookie is cleared and the user lands back on /login. Keeping the
// cookie alive would let another submission re-trigger the failing link.
func TestCompleteOIDCLinkConfirmationConfirmLinkErrorMappingClearsCookie(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.confirmLinkErr = services.ErrOIDCUnavailable
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "link-fail@example.com", "StrongPass1", true)

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-fail", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login on confirm-link failure, got %q", location)
	}
	clearedLinkCookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName)
	if clearedLinkCookie == nil {
		t.Fatal("expected link-pending cookie to be cleared on confirm-link failure")
	}
	if clearedLinkCookie.Value != "" {
		t.Fatalf("expected link-pending cookie to be cleared, got %q", clearedLinkCookie.Value)
	}
}

// TestMapOIDCLinkConfirmError locks the contract from
// ConfirmAndLinkIdentity-failure -> APIErrorSpec. The handler relies on this
// mapping to pick the correct flash key/status; if the mapping drifts an
// unrelated user-facing error message could surface and undermine the
// post-confirmation UX.
func TestMapOIDCLinkConfirmError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{name: "link failed maps to unavailable", err: services.ErrOIDCLinkFailed, want: authOIDCUnavailableErrorSpec()},
		{name: "identity resolve failed maps to unavailable", err: services.ErrOIDCIdentityResolveFailed, want: authOIDCUnavailableErrorSpec()},
		{name: "oidc disabled maps to unavailable", err: services.ErrOIDCDisabled, want: authOIDCUnavailableErrorSpec()},
		{name: "oidc unavailable maps to unavailable", err: services.ErrOIDCUnavailable, want: authOIDCUnavailableErrorSpec()},
		{name: "unknown error falls back to authentication failed", err: errors.New("unmapped storage error"), want: authOIDCAuthenticationFailedErrorSpec()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mapOIDCLinkConfirmError(tt.err)
			if got.Key != tt.want.Key {
				t.Fatalf("expected error key %q, got %q", tt.want.Key, got.Key)
			}
			if got.Status != tt.want.Status {
				t.Fatalf("expected status %d, got %d", tt.want.Status, got.Status)
			}
		})
	}
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

// Step-up 2FA gate on /auth/oidc/link-confirm. Audit finding HIGH-1: handler
// was issuing an auth cookie after the password challenge without ever
// running the TOTP factor, while the canonical Login path (LoginService.
// Authenticate → setTOTPPendingCookie → /auth/2fa) gates session issuance
// behind TOTP for TOTPEnabled users. Attacker with the victim's password
// plus a malicious / sloppy upstream IdP could obtain a session and a
// persistent linked OIDC identity bypassing 2FA. These tests lock the
// closure of that bypass: TOTP-enabled targets MUST present a valid 6-digit
// code together with the password before the link is persisted and a
// session is issued.

func TestCompleteOIDCLinkConfirmationWithTOTPEnabledRequiresValidCode(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "link-totp-valid@example.com", "StrongPass1", true)
	rawSecret := setupTOTPForUser(t, database, user.ID, []byte(testHandlerSecretKey))

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-totp-valid", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	code, err := totp.GenerateCode(rawSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password":  {"StrongPass1"},
		"totp_code": {code},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected /dashboard after valid password+TOTP, got %q", location)
	}
	if stub.lastConfirmLinkUserID != user.ID {
		t.Fatalf("expected ConfirmAndLinkIdentity to receive user id %d, got %d", user.ID, stub.lastConfirmLinkUserID)
	}
	authCookie := responseCookie(response.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected auth cookie after valid password+TOTP")
	}
}

// TestCompleteOIDCLinkConfirmationWithTOTPEnabledRefusesMissingCode is the
// direct anti-regression for HIGH-1: same shape as before (sealed pending
// cookie + correct password) but no totp_code field. The handler MUST NOT
// invoke ConfirmAndLinkIdentity and MUST NOT issue an auth cookie.
func TestCompleteOIDCLinkConfirmationWithTOTPEnabledRefusesMissingCode(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "link-totp-missing@example.com", "StrongPass1", true)
	_ = setupTOTPForUser(t, database, user.ID, []byte(testHandlerSecretKey))

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-totp-missing", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
		// no totp_code field
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != oidcLinkConfirmPath {
		t.Fatalf("expected redirect back to link-confirm on missing TOTP, got %q", location)
	}
	if stub.lastConfirmLinkUserID != 0 {
		t.Fatalf("did not expect ConfirmAndLinkIdentity to fire without TOTP, got user id %d", stub.lastConfirmLinkUserID)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("AUDIT-CRITICAL: link-confirm issued auth cookie without TOTP for TOTPEnabled user")
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil {
		t.Fatal("expected flash cookie with TOTP error")
	}
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != totpInvalidCodeErrorSpec().Key {
		t.Fatalf("expected flash auth_error %q, got %q", totpInvalidCodeErrorSpec().Key, payload.AuthError)
	}
}

func TestCompleteOIDCLinkConfirmationWithTOTPEnabledRefusesWrongCode(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "link-totp-wrong@example.com", "StrongPass1", true)
	_ = setupTOTPForUser(t, database, user.ID, []byte(testHandlerSecretKey))

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-totp-wrong", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password":  {"StrongPass1"},
		"totp_code": {"000000"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != oidcLinkConfirmPath {
		t.Fatalf("expected redirect back to link-confirm on wrong TOTP, got %q", location)
	}
	if stub.lastConfirmLinkUserID != 0 {
		t.Fatalf("did not expect ConfirmAndLinkIdentity to fire with wrong TOTP, got user id %d", stub.lastConfirmLinkUserID)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("AUDIT-CRITICAL: link-confirm issued auth cookie with wrong TOTP")
	}
}

// alignToFreshTOTPStep sleeps into the next RFC 6238 step when the current one is
// nearly over. The replay test below must land BOTH submissions in the same
// 30-second step: each pays a bcrypt password check, so a run starting near a
// boundary would cross it and the "replayed" code would legitimately belong to the
// next step — a flake, not a finding. The margin only ever costs the tail of an
// expiring step, and this is step arithmetic, not a wall-clock assertion.
func alignToFreshTOTPStep() {
	const stepSeconds = int64(30)
	const marginSeconds = int64(3)

	if remaining := stepSeconds - time.Now().Unix()%stepSeconds; remaining <= marginSeconds {
		time.Sleep(time.Duration(remaining) * time.Second)
	}
}

// TestCompleteOIDCLinkConfirmationWithTOTPEnabledRefusesReplayedCode closes the
// last unproven clause of this flow's matrix row, which claimed that "missing /
// wrong / replayed codes refuse the link" while only missing and wrong had tests.
//
// Replay held by construction rather than by coverage: the handler calls
// TOTPService.ValidateCode, which claims the step through an atomic
// `UPDATE … WHERE totp_last_used_step < ?`. Nothing pinned that choice, so a
// refactor to a validate-only helper would keep every other test in this file
// green while turning one captured code into a reusable one for the rest of its
// 30-second window — on the surface that hands out a session for a linked account.
//
// The second attempt carries a FRESH pending cookie deliberately: the step floor
// lives on the user row, not in the link state, so the refusal must hold even when
// everything around it is new.
func TestCompleteOIDCLinkConfirmationWithTOTPEnabledRefusesReplayedCode(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "link-totp-replay@example.com", "StrongPass1", true)
	rawSecret := setupTOTPForUser(t, database, user.ID, []byte(testHandlerSecretKey))

	alignToFreshTOTPStep()
	code, err := totp.GenerateCode(rawSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	confirmWithCode := func(subject string) *http.Response {
		t.Helper()

		pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", subject, user.Email)
		if err != nil {
			t.Fatalf("newOIDCLinkPendingPayload: %v", err)
		}
		postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
			"password":  {"StrongPass1"},
			"totp_code": {code},
		}.Encode()))
		postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+sealLinkPendingCookieForTest(t, pendingPayload))
		return mustAppResponse(t, app, postRequest)
	}

	first := confirmWithCode("subject-totp-replay-first")
	assertStatusCode(t, first, http.StatusSeeOther)
	if location := first.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("test setup: the first use of a valid code must link, got %q", location)
	}

	// Forget the first link so the assertion below cannot pass on a stale value.
	stub.lastConfirmLinkUserID = 0

	second := confirmWithCode("subject-totp-replay-second")
	assertStatusCode(t, second, http.StatusSeeOther)
	if location := second.Header.Get("Location"); location != oidcLinkConfirmPath {
		t.Fatalf("expected a replayed TOTP code to bounce back to link-confirm, got %q", location)
	}
	if stub.lastConfirmLinkUserID != 0 {
		t.Fatalf("ConfirmAndLinkIdentity fired on a replayed TOTP code (user id %d)", stub.lastConfirmLinkUserID)
	}
	if authCookie := responseCookie(second.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("AUDIT-CRITICAL: link-confirm issued an auth cookie for a replayed TOTP code")
	}
}

// TestShowOIDCLinkConfirmPageRendersTOTPFieldForTOTPEnabledTarget locks the
// page-render contract: the TOTP input must appear only when the target
// account actually has TOTP enabled. Otherwise the handler-level enforcement
// is unreachable from the UI for legitimate users.
func TestShowOIDCLinkConfirmPageRendersTOTPFieldForTOTPEnabledTarget(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})
	user := createOnboardingTestUser(t, database, "link-totp-render@example.com", "StrongPass1", true)
	_ = setupTOTPForUser(t, database, user.ID, []byte(testHandlerSecretKey))

	payload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-render", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, payload)

	request := httptest.NewRequest(http.MethodGet, oidcLinkConfirmPath, nil)
	request.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	if !strings.Contains(body, `data-link-confirm-totp`) {
		t.Fatalf("expected TOTP input wrapper for TOTPEnabled target, got body without data-link-confirm-totp")
	}
	if !strings.Contains(body, `name="totp_code"`) {
		t.Fatalf("expected totp_code field on link-confirm form for TOTPEnabled target")
	}
}

// TestShowOIDCLinkConfirmPageHidesTOTPFieldForNonTOTPTarget guards against
// the inverse mistake: the TOTP input must not appear for accounts that did
// not enable TOTP, otherwise the form would block legitimate confirmations.
func TestShowOIDCLinkConfirmPageHidesTOTPFieldForNonTOTPTarget(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})
	user := createOnboardingTestUser(t, database, "link-no-totp-render@example.com", "StrongPass1", true)

	payload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-render-no-totp", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, payload)

	request := httptest.NewRequest(http.MethodGet, oidcLinkConfirmPath, nil)
	request.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	if strings.Contains(body, `data-link-confirm-totp`) {
		t.Fatalf("did not expect TOTP input wrapper for non-TOTPEnabled target, got %q", body)
	}
}

// TestCompleteOIDCLinkConfirmationEmitsAuditLogOnSuccess locks the audit
// emission contract from SECURITY.md "Logging Constraints": every
// auth-link-confirm success/failure transitions through
// handler.logSecurityEvent / logSecurityError. Without this regression a
// future refactor that swallows the "linked" event would leave the
// post-incident audit trail silently incomplete.
func TestCompleteOIDCLinkConfirmationEmitsAuditLogOnSuccess(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure:    true,
		oidcService:     stub,
		auditLogEnabled: true,
	})
	user := createOnboardingTestUser(t, database, "link-audit@example.com", "StrongPass1", true)

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-audit", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)

	logged := output.String()
	if !strings.Contains(logged, `action="auth.oidc_link_confirm"`) {
		t.Fatalf("expected auth.oidc_link_confirm action in audit log, got %q", logged)
	}
	if !strings.Contains(logged, `outcome="linked"`) {
		t.Fatalf("expected outcome=linked in audit log after successful link, got %q", logged)
	}
}

// TestCompleteOIDCLinkConfirmationRejectsRequestWithoutCSRFToken closes the
// security.md "every state-mutating endpoint MUST be CSRF-protected at the
// middleware layer and have a regression confirming 403 when the csrf_token
// form field is missing" invariant for /auth/oidc/link-confirm. The other
// link-confirm handler regressions run on a no-CSRF app and only cover
// handler-level behavior; this test is the route-level lock.
func TestCompleteOIDCLinkConfirmationRejectsRequestWithoutCSRFToken(t *testing.T) {
	app, _ := newOnboardingTestAppWithCSRF(t)

	request := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware to reject link-confirm POST without csrf_token (403), got %d", response.StatusCode)
	}
}

// TestMapOIDCLinkConfirmPasswordError pins the password-verification error
// contract of the link-confirm step: rate-limited maps to 429 with the
// shared too-many-attempts key, reset-token issuance failures map to the
// reset-token spec, and every other failure (wrong password, unknown error)
// collapses into the generic invalid-password response.
func TestMapOIDCLinkConfirmPasswordError(t *testing.T) {
	t.Parallel()

	if got := mapOIDCLinkConfirmPasswordError(services.ErrAuthLoginRateLimited); got != authOIDCLinkConfirmRateLimitedErrorSpec() {
		t.Fatalf("rate-limited error mapped to %+v", got)
	}
	if got := mapOIDCLinkConfirmPasswordError(services.ErrLoginResetTokenIssue); got != authResetTokenCreateErrorSpec() {
		t.Fatalf("reset-token issue mapped to %+v", got)
	}
	if got := mapOIDCLinkConfirmPasswordError(services.ErrAuthInvalidCreds); got != authOIDCLinkConfirmInvalidPasswordErrorSpec() {
		t.Fatalf("invalid password mapped to %+v", got)
	}
	if got := mapOIDCLinkConfirmPasswordError(errors.New("boom")); got != authOIDCLinkConfirmInvalidPasswordErrorSpec() {
		t.Fatalf("unknown error mapped to %+v", got)
	}
}

// TestCompleteOIDCLinkConfirmationRateLimitsPasswordAttempts pins the
// link-confirm password throttle: the endpoint verifies credentials through
// the same LoginService attempt policy as the login form, so once the
// per-(client, identity) failure budget is exhausted even the CORRECT
// password is refused with the rate-limited error and no session is issued.
// Without this, link-confirm was a faster password oracle than login,
// bounded only by the per-IP HTTP limiter.
func TestCompleteOIDCLinkConfirmationRateLimitsPasswordAttempts(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})
	user := createOnboardingTestUser(t, database, "link-throttle@example.com", "StrongPass1", true)

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-throttle", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	postWithPassword := func(password string) *http.Response {
		request := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
			"password": {password},
		}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)
		return mustAppResponse(t, app, request)
	}

	// Exhaust the shared login failure budget with wrong passwords.
	for range services.DefaultLoginAttemptsLimit {
		response := postWithPassword("WrongPass2")
		assertStatusCode(t, response, http.StatusSeeOther)
	}

	// The correct password must now be refused with the rate-limited error.
	response := postWithPassword("StrongPass1")
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != oidcLinkConfirmPath {
		t.Fatalf("expected redirect back to link-confirm when rate limited, got %q", location)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect auth cookie while rate limited")
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected flash cookie with rate-limited error")
	}
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != authOIDCLinkConfirmRateLimitedErrorSpec().Key {
		t.Fatalf("expected flash auth_error %q, got %q", authOIDCLinkConfirmRateLimitedErrorSpec().Key, payload.AuthError)
	}
}
