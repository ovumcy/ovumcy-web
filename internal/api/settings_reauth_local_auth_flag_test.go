package api

import (
	"bytes"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
)

// WEB-112: sign-in (AuthenticateCredentials) refuses an account whose
// local_auth_enabled is off even when a password hash is stored. Every
// password re-auth in settings must refuse that account the same way — as an
// account with no local password — or a session on it could prove a password
// sign-in itself no longer accepts.

const signInDisabledFixturePassword = "StrongPass1"

// newSignInDisabledSettingsContext builds an owner with OIDC enabled, a linked
// identity, and a stored hash that matches signInDisabledFixturePassword while
// local_auth_enabled stays false. Audit logging is on so the re-auth cause can
// be read back.
func newSignInDisabledSettingsContext(t *testing.T, email string) (settingsSecurityTestContext, *oidcStepupFixture) {
	t.Helper()
	fixture := newOIDCStepupFixtureWithAudit(t, email, true)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(signInDisabledFixturePassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash fixture password: %v", err)
	}
	if err := fixture.database.Model(&models.User{}).Where("id = ?", fixture.user.ID).Updates(map[string]any{
		"password_hash":      string(passwordHash),
		"local_auth_enabled": false,
	}).Error; err != nil {
		t.Fatalf("store the fixture hash: %v", err)
	}

	var persisted models.User
	if err := fixture.database.First(&persisted, fixture.user.ID).Error; err != nil {
		t.Fatalf("reload fixture user: %v", err)
	}
	if persisted.LocalAuthEnabled {
		t.Fatal("anchor: the fixture must keep local sign-in off")
	}
	if bcrypt.CompareHashAndPassword([]byte(persisted.PasswordHash), []byte(signInDisabledFixturePassword)) != nil {
		t.Fatal("anchor: the stored hash must match the password the routes are sent")
	}

	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	return settingsSecurityTestContext{
		app:        fixture.app,
		database:   fixture.database,
		user:       persisted,
		authCookie: fixture.authCookie,
		csrfCookie: csrfCookie,
		csrfToken:  csrfToken,
	}, fixture
}

// TestSettingsReauthRefusesAStoredHashWhileLocalSignInIsOff drives every
// password re-auth route with the account's CORRECT password. Each must refuse
// with the answer a wrong password gets, log the no_local_password cause (only
// the equalized branch returns it), and change nothing. The password change
// keeps its own earlier "oidc reauth required" gate, which answers before any
// compare and so carries no cause.
func TestSettingsReauthRefusesAStoredHashWhileLocalSignInIsOff(t *testing.T) {
	passwordForm := url.Values{"password": {signInDisabledFixturePassword}}

	cases := []struct {
		name       string
		method     string
		path       func(fixture *oidcStepupFixture) string
		form       url.Values
		action     string
		wantStatus int
		wantKey    string
		wantCause  string
	}{
		{"clear-data validate", http.MethodPost, fixedPath("/api/v1/users/current/data-wipe/validate"), passwordForm, clearDataValidateAction, http.StatusUnauthorized, "invalid password", "no_local_password"},
		{"clear data", http.MethodPost, fixedPath("/api/v1/users/current/data-wipe"), passwordForm, clearDataMutation.action, http.StatusUnauthorized, "invalid password", "no_local_password"},
		{"delete account", http.MethodDelete, fixedPath("/api/v1/users/current"), passwordForm, deleteAccountMutation.action, http.StatusUnauthorized, "invalid password", "no_local_password"},
		{"recovery code regenerate", http.MethodPost, fixedPath("/api/v1/users/current/recovery-code"), passwordForm, "auth.recovery_code_regenerate", http.StatusUnauthorized, "invalid password", "no_local_password"},
		{"2FA enrollment verify", http.MethodPut, fixedPath("/api/v1/users/current/2fa"), passwordForm, "settings.2fa.verify", http.StatusUnauthorized, "invalid password", "no_local_password"},
		{"2FA disable", http.MethodDelete, fixedPath(disableTOTPPath), passwordForm, "settings.2fa.disable", http.StatusUnauthorized, disableTOTPInvalidKey, "no_local_password"},
		{"OIDC identity link step-up", http.MethodPost, fixedPath("/api/v1/users/current/oidc/link/step-up"), passwordForm, oidcIdentityLinkStepupAction, http.StatusUnauthorized, "invalid password", "no_local_password"},
		{"OIDC identity unlink", http.MethodDelete, func(fixture *oidcStepupFixture) string {
			return "/api/v1/users/current/oidc/identities/" + strconv.FormatUint(uint64(fixture.identity.ID), 10)
		}, passwordForm, oidcIdentityUnlinkAction, http.StatusUnauthorized, "invalid password", "no_local_password"},
		{"password change", http.MethodPut, fixedPath("/api/v1/users/current/password"), url.Values{
			"current_password": {signInDisabledFixturePassword},
			"new_password":     {"EvenStronger2"},
			"confirm_password": {"EvenStronger2"},
		}, "auth.password_change", http.StatusForbidden, "oidc reauth required", ""},
	}

	originalWriter := log.Writer()
	t.Cleanup(func() { log.SetOutput(originalWriter) })

	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, fixture := newSignInDisabledSettingsContext(t, "reauth-sign-in-off-"+strconv.Itoa(index)+"@example.com")
			enableTOTPForSettingsTest(t, &ctx)
			before := reloadSignInDisabledUser(t, ctx)

			var captured bytes.Buffer
			log.SetOutput(&captured)
			response := settingsFormRequestWithCSRF(t, ctx, tc.method, tc.path(fixture), tc.form, map[string]string{"Accept": "application/json"})
			status := response.StatusCode
			key := readAPIError(t, response.Body)
			log.SetOutput(originalWriter)

			if status != tc.wantStatus || key != tc.wantKey {
				t.Fatalf("correct password with local sign-in off: got %d %q, want %d %q", status, key, tc.wantStatus, tc.wantKey)
			}
			line := securityEventLine(t, captured.String(), tc.action, "denied")
			if tc.wantCause == "" {
				if strings.Contains(line, "reauth_cause=") {
					t.Fatalf("expected no reauth_cause on a refusal decided before any compare, got %q", line)
				}
			} else if !strings.Contains(line, `reauth_cause="`+tc.wantCause+`"`) {
				t.Fatalf("expected reauth_cause=%s, got %q", tc.wantCause, line)
			}

			after := reloadSignInDisabledUser(t, ctx)
			if after.PasswordHash != before.PasswordHash || after.LocalAuthEnabled ||
				after.RecoveryCodeHash != before.RecoveryCodeHash || !after.TOTPEnabled ||
				after.AuthSessionVersion != before.AuthSessionVersion {
				t.Fatalf("a refused re-auth changed the account: before=%+v after=%+v", before, after)
			}
			var identities int64
			if err := ctx.database.Model(&models.OIDCIdentity{}).Where("user_id = ?", ctx.user.ID).Count(&identities).Error; err != nil {
				t.Fatalf("count identities: %v", err)
			}
			if identities != 1 {
				t.Fatalf("a refused re-auth changed the linked identities: got %d, want 1", identities)
			}
			if fixture.oidcStub.lastReauthState != "" {
				t.Fatal("a refused re-auth started a provider step-up")
			}
		})
	}
}

// TestDisableTOTP2FASignInDisabledRefusalDrawsTheBudget matches the empty-hash
// rule (TestDisableTOTP2FAWithoutALocalPasswordIsRefusedAndDrawsTheBudget): the
// refusal spent an equalized compare, so each one is booked against
// totp.disable, and the correct password is refused once the budget is gone.
func TestDisableTOTP2FASignInDisabledRefusalDrawsTheBudget(t *testing.T) {
	ctx, _ := newSignInDisabledSettingsContext(t, "totp-disable-sign-in-off@example.com")
	enableTOTPForSettingsTest(t, &ctx)

	for attempt := range services.DefaultTOTPDisableAttemptsLimit {
		resp := sendDisableTOTP(t, ctx, signInDisabledFixturePassword)
		assertDisableTOTPRefused(t, resp, http.StatusUnauthorized, disableTOTPInvalidKey, "local sign-in off, attempt "+strconv.Itoa(attempt+1))
	}
	resp := sendDisableTOTP(t, ctx, signInDisabledFixturePassword)
	assertDisableTOTPRefused(t, resp, http.StatusTooManyRequests, disableTOTPRateLimitedKey, "local sign-in off after the budget")
	if !totpEnabledInDatabase(t, ctx) {
		t.Fatal("an account with local sign-in off disabled 2FA")
	}
}

func fixedPath(path string) func(*oidcStepupFixture) string {
	return func(*oidcStepupFixture) string { return path }
}

func reloadSignInDisabledUser(t *testing.T, ctx settingsSecurityTestContext) models.User {
	t.Helper()
	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return reloaded
}
