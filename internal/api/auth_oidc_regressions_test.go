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

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
)

type stubOIDCWorkflowService struct {
	enabled                bool
	localPublicAuthEnabled bool
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
		bodyStringMatch{fragment: "data-auth-sso-cta", message: "expected SSO CTA marker in login page"},
		bodyStringMatch{fragment: "Sign in with SSO", message: "expected localized SSO CTA copy"},
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

// TestOIDCCallbackPendingLinkSealsCookieAndRedirectsToConfirmPage proves the
// hand-off path: when service.Authenticate returns
// ErrOIDCLinkRequiresConfirmation with a target local user, the callback must
// seal the link-pending cookie and redirect to /auth/oidc/link-confirm — not
// silently link or issue an auth session.
func TestOIDCCallbackPendingLinkSealsCookieAndRedirectsToConfirmPage(t *testing.T) {
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
	if location := response.Header.Get("Location"); location != oidcLinkConfirmPath {
		t.Fatalf("expected redirect to link-confirm page, got %q", location)
	}
	linkCookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName)
	if linkCookie == nil || strings.TrimSpace(linkCookie.Value) == "" {
		t.Fatal("expected sealed link-pending cookie on confirmation hand-off")
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("did not expect auth cookie to be issued before password challenge, got %q", authCookie.Value)
	}
}

// TestOIDCCallbackPendingLinkForOIDCOnlyUserRefusesWithoutCookie locks the
// rule that pending-link confirmation requires a usable local password. If
// the target account has LocalAuthEnabled=false, the password challenge
// could never succeed; the handler must refuse rather than strand the user.
func TestOIDCCallbackPendingLinkForOIDCOnlyUserRefusesWithoutCookie(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.result = services.OIDCLoginResult{
		User: models.User{
			ID:                 31,
			Role:               models.RoleOwner,
			AuthSessionVersion: 1,
			LocalAuthEnabled:   false,
			Email:              "oidc-only@example.com",
		},
		PendingLinkClaims: &security.OIDCClaims{
			Issuer:  "https://idp.example",
			Subject: "subject-only",
			Email:   "oidc-only@example.com",
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
		t.Fatalf("expected redirect to /login for OIDC-only target, got %q", location)
	}
	if linkCookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName); linkCookie != nil && strings.TrimSpace(linkCookie.Value) != "" {
		t.Fatalf("did not expect link-pending cookie to be issued for OIDC-only target, got %q", linkCookie.Value)
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
		tt := tt
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
	for attempt := 0; attempt < services.DefaultLoginAttemptsLimit; attempt++ {
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
