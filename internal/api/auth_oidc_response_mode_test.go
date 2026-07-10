package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// auth_oidc_response_mode_test.go covers the transport half of
// OIDC_RESPONSE_MODE=query: the callback is additionally served over GET, its
// parameters are read strictly from the query string (never the body), and the
// sealed one-time state cookie remains the sole authorization check. form_post
// stays POST-only and reads strictly from the body.

func newOwnerOIDCLoginResult() services.OIDCLoginResult {
	return services.OIDCLoginResult{
		User: models.User{
			ID:                  11,
			Role:                models.RoleOwner,
			AuthSessionVersion:  1,
			OnboardingCompleted: true,
		},
		NewlyLinked: true,
	}
}

// startQueryModeOIDC builds a query-mode app, drives /auth/oidc/start, and
// returns the app, the stub, and the sealed state cookie for the callback.
func startQueryModeOIDC(t *testing.T) (app *fiber.App, stub *stubOIDCWorkflowService, stateCookie string, state string) {
	t.Helper()

	stub = newStubOIDCWorkflowService(true)
	stub.responseMode = security.OIDCResponseModeQuery
	stub.authURL = "https://id.example.com/authorize"
	stub.result = newOwnerOIDCLoginResult()

	builtApp, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	startResponse := mustAppResponse(t, builtApp, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	assertStatusCode(t, startResponse, http.StatusTemporaryRedirect)
	cookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if cookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}
	// The PKCE verifier lives ONLY in this sealed state cookie, never in
	// transport — the cookie must be HttpOnly so scripts cannot read it, which
	// is what keeps a query-mode code in the URL inert.
	if !cookie.HttpOnly {
		t.Fatal("OIDC state cookie carrying the PKCE verifier must be HttpOnly")
	}
	if stub.lastStartVerifier != "" && strings.Contains(cookie.Value, stub.lastStartVerifier) {
		t.Fatal("OIDC state cookie must be sealed; the raw PKCE verifier must not appear in plaintext")
	}
	return builtApp, stub, cookie.String(), stub.lastStartState
}

func TestOIDCCallbackQueryModeGETSucceedsWithSealedState(t *testing.T) {
	t.Parallel()

	app, stub, stateCookie, state := startQueryModeOIDC(t)

	target := security.OIDCCallbackPath + "?" + url.Values{
		"state": {state},
		"code":  {"provider-code"},
	}.Encode()
	callbackRequest := httptest.NewRequest(http.MethodGet, target, nil)
	callbackRequest.Header.Set("Cookie", stateCookie)

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected owner redirect to /dashboard, got %q", location)
	}
	if stub.lastAuthCode != "provider-code" {
		t.Fatalf("expected callback code read from query to reach OIDC service, got %q", stub.lastAuthCode)
	}
	authCookie := responseCookie(callbackResponse.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected local auth cookie after successful query-mode GET callback")
	}
}

func TestOIDCCallbackQueryModeGETRejectsForeignState(t *testing.T) {
	t.Parallel()

	app, stub, stateCookie, _ := startQueryModeOIDC(t)

	target := security.OIDCCallbackPath + "?" + url.Values{
		"state": {"not-the-sealed-state"},
		"code":  {"provider-code"},
	}.Encode()
	callbackRequest := httptest.NewRequest(http.MethodGet, target, nil)
	callbackRequest.Header.Set("Cookie", stateCookie)

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login for foreign state, got %q", location)
	}
	if stub.lastAuthCode != "" {
		t.Fatalf("callback must not exchange the code when state mismatches, got %q", stub.lastAuthCode)
	}
}

func TestOIDCCallbackQueryModeGETRejectsMissingStateCookie(t *testing.T) {
	t.Parallel()

	app, stub, _, state := startQueryModeOIDC(t)

	target := security.OIDCCallbackPath + "?" + url.Values{
		"state": {state},
		"code":  {"provider-code"},
	}.Encode()
	// No Cookie header: the sealed state cookie is the sole authorization check.
	callbackResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, target, nil))
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login without state cookie, got %q", location)
	}
	if stub.lastAuthCode != "" {
		t.Fatalf("callback must not exchange the code without a sealed state cookie, got %q", stub.lastAuthCode)
	}
}

// TestOIDCCallbackQueryModeIgnoresBody proves the source is strictly the query
// in query mode: a POST carrying the valid state in its body (empty query) is
// refused because the handler reads the query, not the body.
func TestOIDCCallbackQueryModeIgnoresBody(t *testing.T) {
	t.Parallel()

	app, stub, stateCookie, state := startQueryModeOIDC(t)

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {state},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie)

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("query mode must ignore the body; expected /login, got %q", location)
	}
	if stub.lastAuthCode != "" {
		t.Fatalf("query mode must not read state/code from the body, got code %q", stub.lastAuthCode)
	}
}

// TestOIDCCallbackFormPostModeRejectsGET pins that the default form_post mode
// never registers the GET callback route: a GET falls through to NotFound.
func TestOIDCCallbackFormPostModeRejectsGET(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	callbackResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, security.OIDCCallbackPath, nil))
	assertStatusCode(t, callbackResponse, http.StatusNotFound)
	if stub.lastAuthCode != "" {
		t.Fatalf("form_post GET callback must not run the exchange, got %q", stub.lastAuthCode)
	}
}

// TestOIDCCallbackFormPostModeIgnoresQuery proves the source is strictly the
// body in form_post mode: a POST carrying the valid state only in the query
// string (empty body) is refused because the handler reads the body.
func TestOIDCCallbackFormPostModeIgnoresQuery(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.result = newOwnerOIDCLoginResult()
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	target := security.OIDCCallbackPath + "?" + url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()
	callbackRequest := httptest.NewRequest(http.MethodPost, target, strings.NewReader(""))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("form_post mode must ignore the query; expected /login, got %q", location)
	}
	if stub.lastAuthCode != "" {
		t.Fatalf("form_post mode must not read state/code from the query, got code %q", stub.lastAuthCode)
	}
}
