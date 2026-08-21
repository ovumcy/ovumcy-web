package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

func TestLoginPageInOIDCOnlyModeHidesLocalAuthUI(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.localPublicAuthEnabled = false
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	rendered := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, rendered,
		// Structural hook only — the rendered SSO caption is Playwright's
		// subject (e2e/auth-oidc.spec.ts), sourced from the catalogue.
		bodyStringMatch{fragment: "data-auth-sso-cta", message: "expected SSO CTA in oidc_only mode"},
	)
	assertBodyNotContainsAll(t, rendered,
		bodyStringMatch{fragment: `id="login-form"`, message: "did not expect local login form in oidc_only mode"},
		bodyStringMatch{fragment: `/register`, message: "did not expect register link in oidc_only mode"},
		bodyStringMatch{fragment: `/forgot-password`, message: "did not expect forgot-password link in oidc_only mode"},
	)
}

func TestOIDCOnlyModeRedirectsLocalAuthPagesBackToLogin(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.localPublicAuthEnabled = false
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	for _, path := range []string{"/register", "/forgot-password"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, http.StatusSeeOther)
		if location := response.Header.Get("Location"); location != "/login" {
			t.Fatalf("expected %s to redirect to /login, got %q", path, location)
		}
	}
}

func TestOIDCOnlyModeRejectsLocalPublicAuthEndpoints(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.localPublicAuthEnabled = false
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	testCases := []struct {
		name      string
		path      string
		form      url.Values
		wantError string
	}{
		{
			name:      "login",
			path:      "/api/v1/sessions",
			form:      url.Values{"email": {"owner@example.com"}, "password": {"StrongPass1"}},
			wantError: "local sign-in unavailable",
		},
		{
			name:      "register",
			path:      "/api/v1/users",
			form:      url.Values{"email": {"owner@example.com"}, "password": {"StrongPass1"}, "confirm_password": {"StrongPass1"}},
			wantError: "local sign-in unavailable",
		},
		{
			name:      "forgot-password",
			path:      "/api/v1/password-resets",
			form:      url.Values{"email": {"owner@example.com"}},
			wantError: "local recovery unavailable",
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, testCase.path, strings.NewReader(testCase.form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", "application/json")

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusForbidden)
			if got := readAPIError(t, response.Body); got != testCase.wantError {
				t.Fatalf("expected %q, got %q", testCase.wantError, got)
			}
		})
	}
}

func TestAuthLogoutWithOIDCProviderUsesSameOriginBridge(t *testing.T) {
	t.Parallel()

	app, authCookie, csrfCookie, csrfToken := prepareAuthenticatedOIDCLogoutContext(t, services.OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/oidc/logout",
		IDTokenHint:           "raw-id-token",
		PostLogoutRedirectURL: "https://ovumcy.example.com/login",
	})

	form := url.Values{"csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set(
		"Cookie",
		joinCookieHeader(
			authCookie,
			cookiePair(csrfCookie),
			recoveryCodeCookieName+"=temporary-recovery",
			resetPasswordCookieName+"=temporary-reset",
		),
	)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	location := mustParseLocationHeader(t, response)
	if location.String() != oidcLogoutBridgePath {
		t.Fatalf("expected same-origin logout bridge redirect, got %q", location.String())
	}
	if strings.Contains(location.RawQuery, "id_token_hint") || strings.Contains(location.RawQuery, "post_logout_redirect_uri") {
		t.Fatalf("did not expect provider logout parameters in bridge redirect, got %q", location.String())
	}

	authCookieAfterLogout := responseCookie(response.Cookies(), authCookieName)
	if authCookieAfterLogout == nil || authCookieAfterLogout.Value != "" {
		t.Fatalf("expected logout response to clear auth cookie, got %#v", authCookieAfterLogout)
	}
	bridgeCookieAfterLogout := responseCookie(response.Cookies(), oidcLogoutBridgeCookieName)
	if bridgeCookieAfterLogout == nil || strings.TrimSpace(bridgeCookieAfterLogout.Value) == "" {
		t.Fatalf("expected logout response to set oidc logout bridge cookie, got %#v", bridgeCookieAfterLogout)
	}
	if len(bridgeCookieAfterLogout.Value) > 512 {
		t.Fatalf("expected bounded bridge cookie size, got %d bytes", len(bridgeCookieAfterLogout.Value))
	}
}

func TestAuthLogoutWithOversizedOIDCProviderStateKeepsBridgeCookieSmall(t *testing.T) {
	t.Parallel()

	rawIDToken := strings.Repeat("idtoken.", 768)
	app, authCookie, csrfCookie, csrfToken := prepareAuthenticatedOIDCLogoutContext(t, services.OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/oidc/logout",
		IDTokenHint:           rawIDToken,
		PostLogoutRedirectURL: "https://ovumcy.example.com/login",
	})

	form := url.Values{"csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	bridgeCookie := responseCookie(response.Cookies(), oidcLogoutBridgeCookieName)
	if bridgeCookie == nil || strings.TrimSpace(bridgeCookie.Value) == "" {
		t.Fatalf("expected bounded bridge cookie for oversized logout state, got %#v", bridgeCookie)
	}
	if len(bridgeCookie.Value) > 512 {
		t.Fatalf("expected oversized id_token_hint to stay out of bridge cookie, got %d bytes", len(bridgeCookie.Value))
	}
	tokenProbe := rawIDToken[:128]
	if strings.Contains(response.Header.Get("Location"), tokenProbe) {
		t.Fatal("did not expect oversized id_token_hint in logout redirect location")
	}
	for _, value := range response.Header.Values("Set-Cookie") {
		if strings.Contains(value, tokenProbe) {
			t.Fatal("did not expect oversized id_token_hint in Set-Cookie headers")
		}
	}
}

func TestOIDCLogoutBridgeRedirectsToProviderEndSessionEndpoint(t *testing.T) {
	t.Parallel()

	app, authCookie, _, _ := prepareAuthenticatedOIDCLogoutContext(t, services.OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/oidc/logout",
		IDTokenHint:           "raw-id-token",
		PostLogoutRedirectURL: "https://ovumcy.example.com/login",
	})
	bridgeClaims := mustExtractAuthSessionClaimsFromCookieHeader(t, authCookie)
	bridgeCookie := mustBuildOIDCLogoutBridgeCookieHeader(t, bridgeClaims.SessionID, bridgeClaims.UserID)

	request := httptest.NewRequest(http.MethodGet, oidcLogoutBridgeRedirectPath, nil)
	request.Header.Set("Cookie", bridgeCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	location := mustParseLocationHeader(t, response)
	if location.Scheme != "https" || location.Host != "id.example.com" || location.Path != "/oidc/logout" {
		t.Fatalf("expected provider logout redirect, got %q", location.String())
	}
	if got := location.Query().Get("id_token_hint"); got != "raw-id-token" {
		t.Fatalf("expected id_token_hint in provider logout redirect, got %q", got)
	}
	if got := location.Query().Get("post_logout_redirect_uri"); got != "https://ovumcy.example.com/login" {
		t.Fatalf("expected post_logout_redirect_uri in provider logout redirect, got %q", got)
	}

	bridgeCookieAfterRedirect := responseCookie(response.Cookies(), oidcLogoutBridgeCookieName)
	if bridgeCookieAfterRedirect == nil || bridgeCookieAfterRedirect.Value != "" {
		t.Fatalf("expected oidc logout bridge cookie to be cleared after redirect, got %#v", bridgeCookieAfterRedirect)
	}
}

func TestOIDCLogoutBridgeRedirectConsumesServerSideState(t *testing.T) {
	t.Parallel()

	app, authCookie, _, _ := prepareAuthenticatedOIDCLogoutContext(t, services.OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/oidc/logout",
		IDTokenHint:           "raw-id-token",
		PostLogoutRedirectURL: "https://ovumcy.example.com/login",
	})
	bridgeClaims := mustExtractAuthSessionClaimsFromCookieHeader(t, authCookie)
	bridgeCookie := mustBuildOIDCLogoutBridgeCookieHeader(t, bridgeClaims.SessionID, bridgeClaims.UserID)

	firstRequest := httptest.NewRequest(http.MethodGet, oidcLogoutBridgeRedirectPath, nil)
	firstRequest.Header.Set("Cookie", bridgeCookie)
	firstResponse := mustAppResponse(t, app, firstRequest)
	assertStatusCode(t, firstResponse, http.StatusSeeOther)

	secondRequest := httptest.NewRequest(http.MethodGet, oidcLogoutBridgeRedirectPath, nil)
	secondRequest.Header.Set("Cookie", bridgeCookie)
	secondResponse := mustAppResponse(t, app, secondRequest)
	assertStatusCode(t, secondResponse, http.StatusSeeOther)
	if location := secondResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected consumed logout state to fall back to /login, got %q", location)
	}
}

func TestOIDCLogoutBridgePageRefreshesToInternalRedirectEndpoint(t *testing.T) {
	t.Parallel()

	app, authCookie, _, _ := prepareAuthenticatedOIDCLogoutContext(t, services.OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/oidc/logout",
		IDTokenHint:           "raw-id-token",
		PostLogoutRedirectURL: "https://ovumcy.example.com/login",
	})
	bridgeClaims := mustExtractAuthSessionClaimsFromCookieHeader(t, authCookie)
	bridgeCookie := mustBuildOIDCLogoutBridgeCookieHeader(t, bridgeClaims.SessionID, bridgeClaims.UserID)

	request := httptest.NewRequest(http.MethodGet, oidcLogoutBridgePath, nil)
	request.Header.Set("Cookie", bridgeCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	rendered := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `http-equiv="refresh"`, message: "expected logout bridge meta refresh"},
		bodyStringMatch{fragment: oidcLogoutBridgeRedirectPath, message: "expected bridge page to refresh to internal redirect path"},
	)
	assertBodyNotContainsAll(t, rendered,
		bodyStringMatch{fragment: "id_token_hint", message: "did not expect provider logout token in bridge page markup"},
		bodyStringMatch{fragment: "post_logout_redirect_uri", message: "did not expect provider logout redirect parameter in bridge page markup"},
	)
}

func TestAuthLogoutJSONWithOIDCProviderReturnsBridgePathWithoutTokenLeak(t *testing.T) {
	t.Parallel()

	app, authCookie, csrfCookie, csrfToken := prepareAuthenticatedOIDCLogoutContext(t, services.OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/oidc/logout",
		IDTokenHint:           "raw-id-token",
		PostLogoutRedirectURL: "https://ovumcy.example.com/login",
	})

	form := url.Values{"csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set(
		"Cookie",
		joinCookieHeader(
			authCookie,
			cookiePair(csrfCookie),
		),
	)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	var payload struct {
		OK       bool   `json:"ok"`
		Redirect string `json:"redirect"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode logout json response: %v", err)
	}
	if !payload.OK {
		t.Fatalf("expected ok=true logout response, got %#v", payload)
	}
	if payload.Redirect != oidcLogoutBridgePath {
		t.Fatalf("expected JSON logout redirect %q, got %q", oidcLogoutBridgePath, payload.Redirect)
	}
	if strings.Contains(payload.Redirect, "id_token_hint") || strings.Contains(payload.Redirect, "post_logout_redirect_uri") {
		t.Fatalf("did not expect provider logout parameters in JSON redirect, got %q", payload.Redirect)
	}
}

// TestOIDCLogoutBridgeRedirectRefusesAnotherOwnersSessionID is the privacy
// boundary on the one route that runs with no session at all. The bridge
// redirect resolves the stored end-session material — the raw id_token_hint
// among it — from the sealed bridge cookie alone, so if that lookup were keyed
// on the session id by itself, a cookie naming owner A and owner B's session
// would hand A a redirect carrying B's id_token_hint and consume B's row in
// passing. The pair (session id, owner) is what resolves the row.
//
// The refusal is negative twice over — no provider redirect, no consumed row —
// so the same test carries its positive anchor: owner B's own bridge cookie,
// against the same app and the same row, does redirect to the provider and
// does consume it.
func TestOIDCLogoutBridgeRedirectRefusesAnotherOwnersSessionID(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithCSRF(t)
	ownerA := createOnboardingTestUser(t, database, "bridge-owner-a@example.com", "StrongPass1", true)
	ownerB := createOnboardingTestUser(t, database, "bridge-owner-b@example.com", "StrongPass1", true)

	authA := loginAndExtractAuthCookieWithCSRF(t, app, ownerA.Email, "StrongPass1")
	authB := loginAndExtractAuthCookieWithCSRF(t, app, ownerB.Email, "StrongPass1")
	claimsA := mustExtractAuthSessionClaimsFromCookieHeader(t, authA)
	claimsB := mustExtractAuthSessionClaimsFromCookieHeader(t, authB)

	persistOIDCLogoutStateForAuthCookie(t, database, authA, services.OIDCLogoutState{
		UserID:                ownerA.ID,
		EndSessionEndpoint:    "https://id.example.com/oidc/logout",
		IDTokenHint:           "owner-a-id-token",
		PostLogoutRedirectURL: "https://ovumcy.example.com/login",
	})
	persistOIDCLogoutStateForAuthCookie(t, database, authB, services.OIDCLogoutState{
		UserID:                ownerB.ID,
		EndSessionEndpoint:    "https://id.example.com/oidc/logout",
		IDTokenHint:           "owner-b-id-token",
		PostLogoutRedirectURL: "https://ovumcy.example.com/login",
	})

	// Owner A's cookie, owner B's session id: the swap the guard exists for.
	swapped := mustBuildOIDCLogoutBridgeCookieHeader(t, claimsB.SessionID, claimsA.UserID)
	request := httptest.NewRequest(http.MethodGet, oidcLogoutBridgeRedirectPath, nil)
	request.Header.Set("Cookie", swapped)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("owner %d presented owner %d's session id and was redirected to %q; the bridge resolves the logout state by session id alone", ownerA.ID, ownerB.ID, location)
	}
	body := mustReadBodyString(t, response.Body)
	for _, header := range response.Header.Values("Set-Cookie") {
		if strings.Contains(header, "owner-b-id-token") {
			t.Fatal("owner B's id_token_hint reached a response to owner A")
		}
	}
	if strings.Contains(body, "owner-b-id-token") {
		t.Fatal("owner B's id_token_hint reached a response body served to owner A")
	}

	// Owner B's row must still be there: a cross-owner attempt may not consume
	// what it could not read.
	logoutRepo := db.NewRepositories(database).OIDCLogout
	if _, found, err := logoutRepo.FindBySessionID(context.Background(), claimsB.SessionID, ownerB.ID); err != nil || !found {
		t.Fatalf("owner %d's provider-logout state was consumed by owner %d's request (found=%t, err=%v)", ownerB.ID, ownerA.ID, found, err)
	}

	// Positive anchor: the same row, reached by the owner it belongs to.
	ownCookie := mustBuildOIDCLogoutBridgeCookieHeader(t, claimsB.SessionID, claimsB.UserID)
	ownRequest := httptest.NewRequest(http.MethodGet, oidcLogoutBridgeRedirectPath, nil)
	ownRequest.Header.Set("Cookie", ownCookie)

	ownResponse := mustAppResponse(t, app, ownRequest)
	assertStatusCode(t, ownResponse, http.StatusSeeOther)
	location := mustParseLocationHeader(t, ownResponse)
	if location.Host != "id.example.com" {
		t.Fatalf("owner %d must reach the provider end-session endpoint with their own bridge cookie, got %q", ownerB.ID, location.String())
	}
	if got := location.Query().Get("id_token_hint"); got != "owner-b-id-token" {
		t.Fatalf("expected owner %d's own id_token_hint in their provider logout redirect, got %q", ownerB.ID, got)
	}
	if _, found, err := logoutRepo.FindBySessionID(context.Background(), claimsB.SessionID, ownerB.ID); err != nil || found {
		t.Fatalf("owner %d's own bridge redirect must consume the row (found=%t, err=%v)", ownerB.ID, found, err)
	}
}

// TestOIDCLogoutBridgeRedirectDegradesALegacyBridgeCookieToLocalSignOut pins
// the accepted failure mode of adding the owner to this payload. A bridge
// cookie minted by the previous build names a session and no account; a zero
// owner is refused rather than read as "any owner", so that browser gets a
// local sign-out at /login instead of a provider end-session. The window is
// bounded by the payload's own one-minute TTL, so it covers only cookies
// minted in the minute before the deploy — and the row it could not consume is
// left standing for the server-side TTL sweep rather than erroring or being
// deleted by session id alone.
func TestOIDCLogoutBridgeRedirectDegradesALegacyBridgeCookieToLocalSignOut(t *testing.T) {
	t.Parallel()

	app, authCookie, _, _ := prepareAuthenticatedOIDCLogoutContext(t, services.OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/oidc/logout",
		IDTokenHint:           "raw-id-token",
		PostLogoutRedirectURL: "https://ovumcy.example.com/login",
	})
	claims := mustExtractAuthSessionClaimsFromCookieHeader(t, authCookie)

	// Marshalled from the previous version's payload shape — no user_id key at
	// all — rather than from a zero field this build would write.
	serialized, err := json.Marshal(struct {
		SessionID     string `json:"session_id"`
		ExpiresAtUnix int64  `json:"expires_at_unix"`
	}{SessionID: claims.SessionID, ExpiresAtUnix: 4102444800})
	if err != nil {
		t.Fatalf("marshal legacy bridge payload: %v", err)
	}
	legacyCookie := oidcLogoutBridgeCookieName + "=" + mustSealOIDCLogoutBridgePayload(t, serialized)

	request := httptest.NewRequest(http.MethodGet, oidcLogoutBridgeRedirectPath, nil)
	request.Header.Set("Cookie", legacyCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("a bridge cookie naming no owner must degrade to a local sign-out, got %q", location)
	}
	bridgeCookieAfter := responseCookie(response.Cookies(), oidcLogoutBridgeCookieName)
	if bridgeCookieAfter == nil || bridgeCookieAfter.Value != "" {
		t.Fatalf("expected the refused bridge cookie to be retracted, got %#v", bridgeCookieAfter)
	}

	// Positive anchor: the same session, the same row, a payload that names the
	// owner — otherwise a bridge that redirected everything to /login would pass.
	ownCookie := mustBuildOIDCLogoutBridgeCookieHeader(t, claims.SessionID, claims.UserID)
	ownRequest := httptest.NewRequest(http.MethodGet, oidcLogoutBridgeRedirectPath, nil)
	ownRequest.Header.Set("Cookie", ownCookie)

	ownResponse := mustAppResponse(t, app, ownRequest)
	assertStatusCode(t, ownResponse, http.StatusSeeOther)
	location := mustParseLocationHeader(t, ownResponse)
	if location.Host != "id.example.com" {
		t.Fatalf("the same session with an attributed bridge cookie must still reach the provider, got %q", location.String())
	}
}

func prepareAuthenticatedOIDCLogoutContext(t *testing.T, logoutState services.OIDCLogoutState) (*fiber.App, string, *http.Cookie, string) {
	t.Helper()

	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "oidc-logout@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	// The state belongs to the account whose session it is keyed on — the
	// store refuses a row no owner can be reached by, and every read of it is
	// scoped to that owner.
	logoutState.UserID = user.ID
	persistOIDCLogoutStateForAuthCookie(t, database, authCookie, logoutState)

	csrfRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	csrfRequest.Header.Set("Accept-Language", "en")
	csrfRequest.Header.Set("Cookie", authCookie)

	csrfResponse := mustAppResponse(t, app, csrfRequest)
	assertStatusCode(t, csrfResponse, http.StatusOK)
	body := mustReadBodyString(t, csrfResponse.Body)
	csrfCookie := responseCookie(csrfResponse.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatal("expected csrf cookie in dashboard response")
	}

	return app, authCookie, csrfCookie, extractCSRFTokenFromHTML(t, body)
}

func persistOIDCLogoutStateForAuthCookie(t *testing.T, database *gorm.DB, authCookie string, logoutState services.OIDCLogoutState) {
	t.Helper()

	sessionID := mustExtractAuthSessionIDFromCookieHeader(t, authCookie)
	stateService := services.NewOIDCLogoutStateService(db.NewRepositories(database).OIDCLogout)
	if err := stateService.Save(context.Background(), sessionID, logoutState, time.Now().UTC()); err != nil {
		t.Fatalf("save oidc logout state: %v", err)
	}
}

func mustExtractAuthSessionIDFromCookieHeader(t *testing.T, authCookie string) string {
	t.Helper()

	return mustExtractAuthSessionClaimsFromCookieHeader(t, authCookie).SessionID
}

// mustExtractAuthSessionClaimsFromCookieHeader opens the sealed auth cookie and
// returns the session token's claims. Callers that build a provider-logout
// bridge cookie need both halves of what that payload names — the session id
// and the owner — and taking them from the same token keeps the pair
// consistent with the session under test.
func mustExtractAuthSessionClaimsFromCookieHeader(t *testing.T, authCookie string) *services.AuthSessionClaims {
	t.Helper()

	_, sealedValue, ok := strings.Cut(strings.TrimSpace(authCookie), "=")
	if !ok || strings.TrimSpace(sealedValue) == "" {
		t.Fatalf("expected auth cookie header, got %q", authCookie)
	}

	codec, err := newSecureCookieCodec([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("init secure cookie codec: %v", err)
	}
	tokenValue, err := codec.open(authCookieName, sealedValue)
	if err != nil {
		t.Fatalf("open sealed auth cookie: %v", err)
	}

	claims, err := services.ParseAuthSessionToken([]byte("test-secret-key"), string(tokenValue), time.Now())
	if err != nil {
		t.Fatalf("parse auth session token: %v", err)
	}
	if strings.TrimSpace(claims.SessionID) == "" {
		t.Fatal("expected auth session token to carry a session id")
	}
	if claims.UserID == 0 {
		t.Fatal("expected auth session token to name the account it was issued for")
	}
	return claims
}

func mustBuildOIDCLogoutBridgeCookieHeader(t *testing.T, sessionID string, userID uint) string {
	t.Helper()

	serialized, err := json.Marshal(oidcLogoutBridgeCookiePayload{
		SessionID:     sessionID,
		UserID:        userID,
		ExpiresAtUnix: 4102444800,
	})
	if err != nil {
		t.Fatalf("marshal oidc logout bridge cookie payload: %v", err)
	}
	return oidcLogoutBridgeCookieName + "=" + mustSealOIDCLogoutBridgePayload(t, serialized)
}

func mustSealOIDCLogoutBridgePayload(t *testing.T, serialized []byte) string {
	t.Helper()

	codec, err := newSecureCookieCodec([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("init secure cookie codec: %v", err)
	}
	sealed, err := codec.seal(oidcLogoutBridgeCookieName, serialized)
	if err != nil {
		t.Fatalf("seal oidc logout bridge cookie payload: %v", err)
	}
	return sealed
}
