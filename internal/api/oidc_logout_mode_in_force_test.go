package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The provider-logout row is a carrier of one session's end-session material,
// never a statement about the mode that produced it. It lives for days, so the
// mode is re-read at sign-out: an instance the operator switched to
// OIDC_LOGOUT_MODE=local, or switched OIDC off on entirely, signs the owner out
// locally from the next request, and drops the row it will not use.
//
// Both refusals are driven end to end over the two routes that could make the
// hop — DELETE /api/v1/sessions/current, which mints the bridge cookie, and
// GET /auth/oidc/logout/redirect, which composes the Location — because a gate
// on only one of them still leaves the other reachable with a hand-built
// bridge cookie.
func TestProviderLogoutFollowsTheConfigurationInForceNotTheStoredRow(t *testing.T) {
	t.Parallel()

	for name, options := range map[string]onboardingTestAppOptions{
		"logout mode switched to local": {
			enableCSRF:     true,
			oidcEnabled:    true,
			oidcLogoutMode: security.OIDCLogoutModeLocal,
		},
		"oidc switched off": {
			enableCSRF:     true,
			oidcEnabled:    false,
			oidcLogoutMode: security.OIDCLogoutModeProvider,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			app, database := newOnboardingTestAppWithOptions(t, options)
			user := createOnboardingTestUser(t, database, "stale-logout-row@example.com", "StrongPass1", true)
			authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
			claims := mustExtractAuthSessionClaimsFromCookieHeader(t, authCookie)
			persistOIDCLogoutStateForAuthCookie(t, database, authCookie, services.OIDCLogoutState{
				UserID:                user.ID,
				EndSessionEndpoint:    testOIDCIssuerURL + "/oidc/logout",
				IDTokenHint:           "stale-id-token",
				PostLogoutRedirectURL: testOIDCPostLogoutRedirectURL,
			})

			// The bridge redirect is reachable without the sign-out that mints
			// the cookie, so it is driven first, against the row still in place.
			bridgeRequest := httptest.NewRequest(http.MethodGet, oidcLogoutBridgeRedirectPath, nil)
			bridgeRequest.Header.Set("Cookie", mustBuildOIDCLogoutBridgeCookieHeader(t, claims.SessionID, claims.UserID))
			bridgeResponse := mustAppResponse(t, app, bridgeRequest)
			assertStatusCode(t, bridgeResponse, http.StatusSeeOther)
			if location := bridgeResponse.Header.Get("Location"); location != "/login" {
				t.Fatalf("%s: the bridge redirect composed a provider end-session Location from a stored row: %q", name, location)
			}
			assertNoStoredIDTokenHintInResponse(t, bridgeResponse, "stale-id-token")

			// The bridge route Consumes the row it reads, so put it back: the
			// sign-out below has to meet a stored row, since "a row is present
			// and the mode no longer wants it" is the whole state under test.
			// Without this the sign-out would find nothing, never enter the
			// discard arm, and the "row is gone" assertion at the end would
			// hold for a reason that has nothing to do with the handler.
			persistOIDCLogoutStateForAuthCookie(t, database, authCookie, services.OIDCLogoutState{
				UserID:                user.ID,
				EndSessionEndpoint:    testOIDCIssuerURL + "/oidc/logout",
				IDTokenHint:           "stale-id-token",
				PostLogoutRedirectURL: testOIDCPostLogoutRedirectURL,
			})
			if _, found, err := db.NewRepositories(database).OIDCLogout.FindBySessionID(context.Background(), claims.SessionID, user.ID); err != nil || !found {
				t.Fatalf("%s: the row under test was not in place before the sign-out (found=%t, err=%v)", name, found, err)
			}

			// Sign out: no bridge cookie, a local /login answer, and the row
			// the instance will not use is gone rather than left for its TTL.
			csrfCookie, csrfToken := mustCSRFPairForAuthCookie(t, app, authCookie)
			form := url.Values{"csrf_token": {csrfToken}}
			logoutRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(form.Encode()))
			logoutRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			logoutRequest.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

			logoutResponse := mustAppResponse(t, app, logoutRequest)
			assertStatusCode(t, logoutResponse, http.StatusSeeOther)
			if location := logoutResponse.Header.Get("Location"); location != "/login" {
				t.Fatalf("%s: sign-out routed through the provider bridge at %q", name, location)
			}
			if bridgeCookie := responseCookie(logoutResponse.Cookies(), oidcLogoutBridgeCookieName); bridgeCookie != nil && bridgeCookie.Value != "" {
				t.Fatalf("%s: sign-out minted a provider-logout bridge cookie: %#v", name, bridgeCookie)
			}
			assertNoStoredIDTokenHintInResponse(t, logoutResponse, "stale-id-token")
			if _, found, err := db.NewRepositories(database).OIDCLogout.FindBySessionID(context.Background(), claims.SessionID, user.ID); err != nil || found {
				t.Fatalf("%s: the stale provider-logout row survived the sign-out (found=%t, err=%v)", name, found, err)
			}
		})
	}
}

// The positive control for the two refusals above: the same row, the same
// routes, on an instance whose configuration still asks for provider logout.
// Without it a build that answered /login to every sign-out would satisfy them
// both.
func TestProviderLogoutStillReachesEndSessionUnderTheProviderMode(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithOptions(t, providerLogoutTestAppOptions())
	user := createOnboardingTestUser(t, database, "live-logout-row@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	claims := mustExtractAuthSessionClaimsFromCookieHeader(t, authCookie)
	persistOIDCLogoutStateForAuthCookie(t, database, authCookie, services.OIDCLogoutState{
		UserID:                user.ID,
		EndSessionEndpoint:    testOIDCIssuerURL + "/oidc/logout",
		IDTokenHint:           "live-id-token",
		PostLogoutRedirectURL: testOIDCPostLogoutRedirectURL,
	})

	csrfCookie, csrfToken := mustCSRFPairForAuthCookie(t, app, authCookie)
	form := url.Values{"csrf_token": {csrfToken}}
	logoutRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(form.Encode()))
	logoutRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logoutRequest.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	logoutResponse := mustAppResponse(t, app, logoutRequest)
	assertStatusCode(t, logoutResponse, http.StatusSeeOther)
	if location := logoutResponse.Header.Get("Location"); location != oidcLogoutBridgePath {
		t.Fatalf("provider mode: sign-out answered %q, want the same-origin bridge %q", location, oidcLogoutBridgePath)
	}
	if bridgeCookie := responseCookie(logoutResponse.Cookies(), oidcLogoutBridgeCookieName); bridgeCookie == nil || strings.TrimSpace(bridgeCookie.Value) == "" {
		t.Fatalf("provider mode: sign-out minted no bridge cookie: %#v", bridgeCookie)
	}

	bridgeRequest := httptest.NewRequest(http.MethodGet, oidcLogoutBridgeRedirectPath, nil)
	bridgeRequest.Header.Set("Cookie", mustBuildOIDCLogoutBridgeCookieHeader(t, claims.SessionID, claims.UserID))
	bridgeResponse := mustAppResponse(t, app, bridgeRequest)
	assertStatusCode(t, bridgeResponse, http.StatusSeeOther)
	location := mustParseLocationHeader(t, bridgeResponse)
	if location.Host != "id.example.com" || location.Path != "/oidc/logout" {
		t.Fatalf("provider mode: the bridge redirect did not reach the provider end-session endpoint: %q", location.String())
	}
	if got := location.Query().Get("id_token_hint"); got != "live-id-token" {
		t.Fatalf("provider mode: id_token_hint = %q, want the stored one", got)
	}
}

// The gate is one predicate, and every mode the configuration can hold is
// decided by it — `auto` included, which is the mode that asks for a provider
// sign-out when the provider offers one and is therefore as live as
// `provider`. Asserted against the production constructor rather than a stub,
// so a mode added later without an answer here shows up as an unlisted case.
func TestProviderLogoutConfiguredReadsTheModeInForce(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		enabled bool
		mode    security.OIDCLogoutMode
		want    bool
	}{
		"provider mode, oidc on": {enabled: true, mode: security.OIDCLogoutModeProvider, want: true},
		"auto mode, oidc on":     {enabled: true, mode: security.OIDCLogoutModeAuto, want: true},
		"local mode, oidc on":    {enabled: true, mode: security.OIDCLogoutModeLocal, want: false},
		"unset mode, oidc on":    {enabled: true, mode: "", want: false},
		"provider mode, oidc off": {
			enabled: false,
			mode:    security.OIDCLogoutModeProvider,
			want:    false,
		},
	} {
		handler := &Handler{oidcService: services.NewOIDCLoginService(
			security.NewOIDCClient(security.OIDCConfig{
				Enabled:               tc.enabled,
				LogoutMode:            tc.mode,
				IssuerURL:             testOIDCIssuerURL,
				PostLogoutRedirectURL: testOIDCPostLogoutRedirectURL,
			}),
			nil, nil, nil,
		)}
		if got := handler.providerLogoutConfigured(); got != tc.want {
			t.Fatalf("%s: providerLogoutConfigured() = %t, want %t", name, got, tc.want)
		}
	}

	if (&Handler{}).providerLogoutConfigured() {
		t.Fatal("a handler with no OIDC service reported provider logout configured")
	}
}

// assertNoStoredIDTokenHintInResponse fails if the stored hint reached the
// browser in any of the places a provider redirect would have put it.
func assertNoStoredIDTokenHintInResponse(t *testing.T, response *http.Response, idTokenHint string) {
	t.Helper()

	if strings.Contains(response.Header.Get("Location"), idTokenHint) {
		t.Fatalf("the stored id_token_hint reached the Location header: %q", response.Header.Get("Location"))
	}
	for _, header := range response.Header.Values("Set-Cookie") {
		if strings.Contains(header, idTokenHint) {
			t.Fatal("the stored id_token_hint reached a Set-Cookie header")
		}
	}
}

// mustCSRFPairForAuthCookie fetches a page as the signed-in owner and returns
// the CSRF cookie and token the sign-out form needs.
func mustCSRFPairForAuthCookie(t *testing.T, app *fiber.App, authCookie string) (*http.Cookie, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	body := mustReadBodyString(t, response.Body)
	csrfCookie := responseCookie(response.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatal("expected a csrf cookie on the dashboard response")
	}
	return csrfCookie, extractCSRFTokenFromHTML(t, body)
}
