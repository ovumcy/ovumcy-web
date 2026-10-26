package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
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
		// The danger zone offers the step-up route rather than a password form:
		// an account with no local password confirms an erasure at the provider.
		// Addressed by hook, not by copy, per the markup-contract rule.
		bodyStringMatch{fragment: "data-clear-data-stepup", message: "expected the clear-data step-up panel for oidc-only account"},
		bodyStringMatch{fragment: `action="/api/v1/users/current/data-wipe/step-up"`, message: "expected the clear-data step-up form action"},
		bodyStringMatch{fragment: "data-delete-account-stepup", message: "expected the delete-account step-up panel for oidc-only account"},
		bodyStringMatch{fragment: `action="/api/v1/users/current/deletion/step-up"`, message: "expected the delete-account step-up form action"},
	)
	assertBodyNotContainsAll(t, rendered,
		bodyStringMatch{fragment: `name="current_password"`, message: "did not expect current-password field before local auth is enabled"},
		bodyStringMatch{fragment: "Regenerate recovery code", message: "did not expect recovery-code regeneration button before local auth is enabled"},
	)
}

// TestOIDCOnlySettingsSensitiveActionsRequireLocalPassword pins WEB-54: an
// OIDC-only account submitting a password to a password-gated settings action
// gets the SAME refusal a wrong password on a local-auth account gets — 401
// "invalid password" — never the old, distinct 403 "local password required"
// that told the caller outright it had no local password to check against.
// Regression: TestSettingsReauthMergesNoLocalPasswordIntoInvalidPassword pins
// the same invariant at the mapper level; this one pins it end to end.
func TestOIDCOnlySettingsSensitiveActionsRequireLocalPassword(t *testing.T) {
	t.Parallel()

	ctx := newOIDCOnlySettingsSecurityTestContext(t, "settings-oidc-guard@example.com")

	testCases := []struct {
		name   string
		method string
		path   string
		form   url.Values
	}{
		{
			name:   "recovery regeneration",
			method: http.MethodPost,
			path:   "/api/v1/users/current/recovery-code",
			form:   url.Values{"password": {"unused"}},
		},
		{
			name:   "clear-data validation",
			method: http.MethodPost,
			path:   "/api/v1/users/current/data-wipe/validate",
			form:   url.Values{"password": {"unused"}},
		},
		{
			name:   "delete account",
			method: http.MethodDelete,
			path:   "/api/v1/users/current",
			form:   url.Values{"password": {"unused"}},
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			response := settingsFormRequestWithCSRF(t, ctx, testCase.method, testCase.path, testCase.form, map[string]string{
				"Accept": "application/json",
			})
			defer func() { _ = response.Body.Close() }()

			assertStatusCode(t, response, http.StatusUnauthorized)
			if got := readAPIError(t, response.Body); got != "invalid password" {
				t.Fatalf("expected %q, got %q", "invalid password", got)
			}
		})
	}
}

// TestOIDCOnlySettingsChangePasswordRequiresReauth is the closed-CVE regression
// for #3: prior to the OIDC step-up flow, POST /api/v1/users/current/password
// for an OIDC-only account silently enabled a local password and emitted a
// fresh recovery code. A hijacked session could thus mint a permanent
// take-over in a single request. The endpoint must now reject this branch
// with 403 oidc-reauth-required and leave the user untouched.
func TestOIDCOnlySettingsChangePasswordRequiresReauth(t *testing.T) {
	t.Parallel()

	ctx := newOIDCOnlySettingsSecurityTestContext(t, "settings-oidc-change-password-blocked@example.com")

	form := url.Values{
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", form, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusForbidden)
	if got := readAPIError(t, response.Body); got != "oidc reauth required" {
		t.Fatalf("expected error %q, got %q", "oidc reauth required", got)
	}

	var persisted models.User
	if err := ctx.database.First(&persisted, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload oidc-only user: %v", err)
	}
	if persisted.LocalAuthEnabled {
		t.Fatal("oidc-only user must remain in non-local-auth state after rejected ChangePassword")
	}
	if strings.TrimSpace(persisted.PasswordHash) != "" {
		t.Fatal("oidc-only user must not have a password hash stored after rejected ChangePassword")
	}
	if strings.TrimSpace(persisted.RecoveryCodeHash) != "" {
		t.Fatal("oidc-only user must not have a recovery code hash stored after rejected ChangePassword")
	}
}

// TestSettingsPageOnAnOIDCOnlyInstanceNamesSSORecoveryAndOffersNoRecoveryCode
// pins WEB-278: under OIDC_LOGIN_MODE=oidc_only both consumers of a recovery
// code — the public recovery flow and local sign-in — are closed, so the
// account page neither offers to mint one (for an account with a local
// password) nor promises one after a local password is set (for an account
// without), and names SSO as the recovery path instead.
func TestSettingsPageOnAnOIDCOnlyInstanceNamesSSORecoveryAndOffersNoRecoveryCode(t *testing.T) {
	t.Parallel()

	oidcOnly := func() onboardingTestAppOptions {
		return onboardingTestAppOptions{oidcService: &stubOIDCWorkflowService{enabled: true, localPublicAuthEnabled: false}}
	}
	settingsPage := func(t *testing.T, app *fiber.App, authCookie string) string {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/settings", nil)
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", authCookie)
		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, http.StatusOK)
		return mustReadBodyString(t, response.Body)
	}

	t.Run("an account without a local password is not promised recovery codes", func(t *testing.T) {
		t.Parallel()
		ctx := newOIDCOnlySettingsSecurityTestContextWithOptions(t, "sso-only-instance-no-password@example.com", oidcOnly())
		rendered := settingsPage(t, ctx.app, ctx.authCookie)
		assertBodyContainsAll(t, rendered,
			bodyStringMatch{fragment: "data-settings-recovery-code-sso-only", message: "expected the SSO recovery notice in place of the recovery-code offer"},
			bodyStringMatch{fragment: "data-settings-local-password-sso-only", message: "expected the local-password copy that promises no recovery code"},
		)
		assertBodyNotContainsAll(t, rendered,
			bodyStringMatch{fragment: "data-settings-recovery-code-unavailable", message: "did not expect the 'available after you set a local password' promise on an oidc_only instance"},
			bodyStringMatch{fragment: `action="/api/v1/users/current/recovery-code"`, message: "did not expect a recovery-code form on an oidc_only instance"},
		)
	})

	t.Run("an account with a local password is not offered a code nothing can redeem", func(t *testing.T) {
		t.Parallel()
		// Local sign-in is closed on this instance, so the session is issued
		// directly for an account that does hold a local password.
		app, database := newOnboardingTestAppWithOptions(t, oidcOnly())
		user := createOnboardingTestUser(t, database, "sso-only-instance-with-password@example.com", "StrongPass1", true)
		rendered := settingsPage(t, app, issueAuthCookieForUser(t, user))
		assertBodyContainsAll(t, rendered,
			bodyStringMatch{fragment: "data-settings-recovery-code-sso-only", message: "expected the SSO recovery notice in place of the regenerate form"},
		)
		assertBodyNotContainsAll(t, rendered,
			bodyStringMatch{fragment: `action="/api/v1/users/current/recovery-code"`, message: "did not expect the regenerate form on an oidc_only instance"},
		)
	})
}
