package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestSettingsPageForOIDCOnlyAccountShowsLocalPasswordSetup(t *testing.T) {
	t.Parallel()

	ctx := newOIDCOnlySettingsSecurityTestContext(t, "settings-oidc-only@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response := mustAppResponse(t, ctx.app, request)
	assertStatusCode(t, response, http.StatusOK)

	rendered := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: "Set a local password", message: "expected local password setup title for oidc-only account"},
		bodyStringMatch{fragment: "Recovery codes become available after you set a local password", message: "expected recovery-code guidance for oidc-only account"},
		bodyStringMatch{fragment: "Set a local password first if you want password-confirmed safety actions", message: "expected danger-zone guidance for oidc-only account"},
	)
	assertBodyNotContainsAll(t, rendered,
		bodyStringMatch{fragment: `name="current_password"`, message: "did not expect current-password field before local auth is enabled"},
		bodyStringMatch{fragment: "Regenerate recovery code", message: "did not expect recovery-code regeneration button before local auth is enabled"},
	)
}

func TestOIDCOnlySettingsSensitiveActionsRequireLocalPassword(t *testing.T) {
	t.Parallel()

	ctx := newOIDCOnlySettingsSecurityTestContext(t, "settings-oidc-guard@example.com")

	testCases := []struct {
		name      string
		method    string
		path      string
		form      url.Values
		wantError string
	}{
		{
			name:      "recovery regeneration",
			method:    http.MethodPost,
			path:      "/api/settings/regenerate-recovery-code",
			form:      url.Values{},
			wantError: "local password required",
		},
		{
			name:      "clear-data validation",
			method:    http.MethodPost,
			path:      "/api/settings/clear-data/validate",
			form:      url.Values{"password": {"unused"}},
			wantError: "local password required",
		},
		{
			name:      "delete account",
			method:    http.MethodDelete,
			path:      "/api/settings/delete-account",
			form:      url.Values{"password": {"unused"}},
			wantError: "local password required",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			response := settingsFormRequestWithCSRF(t, ctx, testCase.method, testCase.path, testCase.form, map[string]string{
				"Accept": "application/json",
			})
			defer response.Body.Close()

			assertStatusCode(t, response, http.StatusForbidden)
			if got := readAPIError(t, response.Body); got != testCase.wantError {
				t.Fatalf("expected %q, got %q", testCase.wantError, got)
			}
		})
	}
}

func TestOIDCOnlySettingsEnableLocalPasswordIssuesRecoveryCode(t *testing.T) {
	t.Parallel()

	ctx := newOIDCOnlySettingsSecurityTestContext(t, "settings-enable-local@example.com")

	form := url.Values{
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/change-password", form, map[string]string{
		"Accept-Language": "en",
	})
	defer response.Body.Close()

	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/recovery-code" {
		t.Fatalf("expected redirect to /recovery-code, got %q", location)
	}

	updatedAuthCookie := ctx.authCookie
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && authCookie.Value != "" {
		updatedAuthCookie = cookiePair(authCookie)
	}
	recoveryCookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName)
	if recoveryCookie == "" {
		t.Fatal("expected recovery-code display cookie after enabling local password")
	}

	recoveryRequest := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	recoveryRequest.Header.Set("Accept-Language", "en")
	recoveryRequest.Header.Set("Cookie", joinCookieHeader(updatedAuthCookie, recoveryCodeCookieName+"="+recoveryCookie))
	recoveryResponse := mustAppResponse(t, ctx.app, recoveryRequest)
	assertStatusCode(t, recoveryResponse, http.StatusOK)
	recoveryPage := mustReadBodyString(t, recoveryResponse.Body)
	assertBodyContainsAll(t, recoveryPage,
		bodyStringMatch{fragment: `id="recovery-code"`, message: "expected dedicated recovery-code page after enabling local password"},
		bodyStringMatch{fragment: "/settings", message: "expected recovery-code continue path to point back to settings"},
	)

	var persisted models.User
	if err := ctx.database.First(&persisted, ctx.user.ID).Error; err != nil {
		t.Fatalf("load updated oidc-only user: %v", err)
	}
	if !persisted.LocalAuthEnabled {
		t.Fatal("expected local auth to be enabled after setting local password")
	}
	if strings.TrimSpace(persisted.PasswordHash) == "" {
		t.Fatal("expected stored password hash after setting local password")
	}
	if strings.TrimSpace(persisted.RecoveryCodeHash) == "" {
		t.Fatal("expected stored recovery code hash after setting local password")
	}

	localLoginCookie := loginAndExtractAuthCookieWithCSRF(t, ctx.app, persisted.Email, "EvenStronger2")
	if strings.TrimSpace(localLoginCookie) == "" {
		t.Fatal("expected local login to work after enabling local password")
	}
}

func TestOIDCOnlySettingsEnableLocalPasswordPreservesProviderLogoutBridge(t *testing.T) {
	t.Parallel()

	ctx := newOIDCOnlySettingsSecurityTestContext(t, "settings-enable-local-logout@example.com")
	persistOIDCLogoutStateForAuthCookie(t, ctx.database, ctx.authCookie, services.OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/oidc/logout",
		IDTokenHint:           strings.Repeat("idtoken.", 128),
		PostLogoutRedirectURL: "https://ovumcy.example.com/login",
	})

	form := url.Values{
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/change-password", form, map[string]string{
		"Accept-Language": "en",
	})
	defer response.Body.Close()

	assertStatusCode(t, response, http.StatusSeeOther)
	updatedAuthCookie := ctx.authCookie
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && authCookie.Value != "" {
		updatedAuthCookie = cookiePair(authCookie)
	}

	logoutForm := url.Values{"csrf_token": {ctx.csrfToken}}
	logoutRequest := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(logoutForm.Encode()))
	logoutRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logoutRequest.Header.Set("Cookie", joinCookieHeader(updatedAuthCookie, cookiePair(ctx.csrfCookie)))

	logoutResponse := mustAppResponse(t, ctx.app, logoutRequest)
	assertStatusCode(t, logoutResponse, http.StatusSeeOther)
	if location := logoutResponse.Header.Get("Location"); location != oidcLogoutBridgePath {
		t.Fatalf("expected provider logout bridge after local password enablement, got %q", location)
	}
	bridgeCookie := responseCookie(logoutResponse.Cookies(), oidcLogoutBridgeCookieName)
	if bridgeCookie == nil || strings.TrimSpace(bridgeCookie.Value) == "" {
		t.Fatalf("expected logout bridge cookie after local password enablement, got %#v", bridgeCookie)
	}
	if len(bridgeCookie.Value) > 512 {
		t.Fatalf("expected bounded logout bridge cookie after local password enablement, got %d bytes", len(bridgeCookie.Value))
	}
}
