package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Under OIDC_LOGIN_MODE=oidc_only both consumers of a recovery code — the
// public password reset and local sign-in — answer 403, so a code minted there
// is a promise nothing honours (WEB-340). The step-up enrollment still sets the
// password and re-issues the session; it mints no code and stages no reveal.
func TestLocalPasswordSetupOnAnOIDCOnlyInstanceMintsNoRecoveryCode(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixtureWithOptions(t, "oidc-only-stepup-no-code@example.com", onboardingTestAppOptions{}, func(stub *stubOIDCWorkflowService) OIDCWorkflowService {
		stub.localPublicAuthEnabled = false
		return stub
	})
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	assertStatusCode(t, startResponse, http.StatusOK)
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()
	assertStatusCode(t, callbackResponse, http.StatusOK)

	if reveal := responseCookie(callbackResponse.Cookies(), recoveryCodeCookieName); reveal != nil && strings.TrimSpace(reveal.Value) != "" {
		t.Fatalf("expected no %s cookie on an oidc_only instance", recoveryCodeCookieName)
	}
	body := mustReadBodyString(t, callbackResponse.Body)
	if strings.Contains(body, "/recovery-code") {
		t.Fatalf("expected the handoff not to target the reveal surface, got %q", body)
	}
	if !strings.Contains(body, "/settings") {
		t.Fatalf("expected the handoff to return to /settings, got %q", body)
	}
	flash := responseCookie(callbackResponse.Cookies(), flashCookieName)
	if flash == nil || strings.TrimSpace(flash.Value) == "" {
		t.Fatal("expected a settings success flash for the enrolled password")
	}
	if got := decodeFlashCookieForTest(t, flash.Value).SettingsSuccess; got != "local_password_set" {
		t.Fatalf("expected settings success %q, got %q", "local_password_set", got)
	}
	authCookie := responseCookie(callbackResponse.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected the enrollment to re-issue the auth cookie past its own session bump")
	}

	var persisted models.User
	if err := fixture.database.First(&persisted, fixture.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !persisted.LocalAuthEnabled || strings.TrimSpace(persisted.PasswordHash) == "" {
		t.Fatal("expected the local password to be enrolled")
	}
	if strings.TrimSpace(persisted.RecoveryCodeHash) != "" {
		t.Fatal("expected an empty recovery_code_hash on an oidc_only instance")
	}
	if persisted.AuthSessionVersion <= fixture.user.AuthSessionVersion {
		t.Fatalf("expected the enrollment to bump auth_session_version: before=%d after=%d", fixture.user.AuthSessionVersion, persisted.AuthSessionVersion)
	}

	probe := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	probe.Header.Set("Cookie", cookiePair(authCookie))
	probeResponse := mustAppResponse(t, fixture.app, probe)
	defer func() { _ = probeResponse.Body.Close() }()
	assertStatusCode(t, probeResponse, http.StatusOK)
}

// The regenerate route is refused on the instance's mode alone, before the
// password is read: the mode is instance-wide, so the answer names nothing
// about the account, and a wrong password must not reach the re-auth budget.
func TestRegenerateRecoveryCodeOnAnOIDCOnlyInstanceIsRefused(t *testing.T) {
	t.Parallel()

	for _, password := range []string{"StrongPass1", "WrongPass9"} {
		t.Run(password, func(t *testing.T) {
			t.Parallel()

			app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
				enableCSRF:  true,
				oidcService: &stubOIDCWorkflowService{enabled: true, localPublicAuthEnabled: false},
			})
			user := createOnboardingTestUser(t, database, "oidc-only-regenerate-"+strings.ToLower(password)+"@example.com", "StrongPass1", true)
			const priorHash = "prior-recovery-hash"
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("recovery_code_hash", priorHash).Error; err != nil {
				t.Fatalf("seed recovery_code_hash: %v", err)
			}
			authCookie := issueAuthCookieForUser(t, user)
			csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)
			ctx := settingsSecurityTestContext{app: app, database: database, user: user, authCookie: authCookie, csrfCookie: csrfCookie, csrfToken: csrfToken}

			response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{
				"password": {password},
			}, map[string]string{"Accept": "application/json"})
			defer func() { _ = response.Body.Close() }()

			assertStatusCode(t, response, http.StatusForbidden)
			if got := readAPIError(t, response.Body); got != "recovery code unavailable" {
				t.Fatalf("expected error %q, got %q", "recovery code unavailable", got)
			}
			if reveal := responseCookie(response.Cookies(), recoveryCodeCookieName); reveal != nil && strings.TrimSpace(reveal.Value) != "" {
				t.Fatalf("expected no %s cookie on a refused regeneration", recoveryCodeCookieName)
			}
			assertRecoveryCodeHashUnchanged(t, ctx, priorHash)

			var reloaded models.User
			if err := database.First(&reloaded, user.ID).Error; err != nil {
				t.Fatalf("reload user: %v", err)
			}
			if reloaded.AuthSessionVersion != user.AuthSessionVersion {
				t.Fatalf("expected auth_session_version unchanged: before=%d after=%d", user.AuthSessionVersion, reloaded.AuthSessionVersion)
			}
		})
	}
}
