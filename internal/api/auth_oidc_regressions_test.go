package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
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
		return services.OIDCLoginResult{}, stub.authErr
	}
	return stub.result, nil
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
