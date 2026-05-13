package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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

// TestOIDCOnlySettingsChangePasswordRequiresReauth is the closed-CVE regression
// for #3: prior to the OIDC step-up flow, POST /api/settings/change-password
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
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/change-password", form, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

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
