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

// TestOIDCCallbackRefusesTheForcedResetForAnAccountWithoutALocalPassword pins
// the R-O1 shape (WEB-401): an OIDC-provisioned account with unverifiable TOTP
// cannot be issued the forced-OIDC token, which is bound to a password
// fingerprint it does not have. The callback lands on /login with no cookie
// that carries a sign-in onward, and the audit line names the cause so the
// operator knows `reset-password --full` is the way back (walked end to end by
// TestResetPasswordFullRecoversAnOIDCOnlyAccountWithUnverifiableTOTP).
func TestOIDCCallbackRefusesTheForcedResetForAnAccountWithoutALocalPassword(t *testing.T) {
	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.result = services.OIDCLoginResult{
		User: models.User{
			ID:                 52,
			Role:               models.RoleOwner,
			AuthSessionVersion: 1,
			LocalAuthEnabled:   false,
			TOTPEnabled:        true,
		},
		RequiresPasswordReset: true,
	}
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure:    true,
		oidcService:     stub,
		auditLogEnabled: true,
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

	callbackResponse, logOutput := captureAuditedRequest(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, fiber.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}
	for _, name := range []string{resetPasswordCookieName, authCookieName, totpPendingCookieName} {
		if c := responseCookie(callbackResponse.Cookies(), name); c != nil && strings.TrimSpace(c.Value) != "" {
			t.Fatalf("did not expect a %s cookie for an account without a local password", name)
		}
	}
	if !strings.Contains(logOutput, "no_local_password") {
		t.Fatalf("expected the no_local_password cause in the audit line, got %q", logOutput)
	}
}

// TestResetRequiredAuditLineNamesItsCause pins R-A4 (WEB-401) at both callers:
// the reset_required line says why the sign-in was routed to the forced reset.
func TestResetRequiredAuditLineNamesItsCause(t *testing.T) {
	t.Run("login", func(t *testing.T) {
		app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{auditLogEnabled: true})
		user := createOnboardingTestUser(t, database, "reset-cause-login@example.com", "StrongPass1", true)
		setupTOTPForUser(t, database, user.ID, []byte(testAppSecretKey))
		rotated, err := security.EncryptField("does-not-matter", []byte("a-completely-different-secret-k"), []byte("irrelevant-aad"))
		if err != nil {
			t.Fatalf("EncryptField: %v", err)
		}
		if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("totp_secret", rotated).Error; err != nil {
			t.Fatalf("corrupt totp_secret: %v", err)
		}

		request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(url.Values{
			"email":    {user.Email},
			"password": {"StrongPass1"},
		}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, logOutput := captureAuditedRequest(t, app, request)
		assertStatusCode(t, response, fiber.StatusSeeOther)
		if line := securityEventLine(t, logOutput, "auth.login", "reset_required"); !strings.Contains(line, "cause") || !strings.Contains(line, services.ResetCauseTOTPUnverifiable) {
			t.Fatalf("expected cause %q on the reset_required line, got %q", services.ResetCauseTOTPUnverifiable, line)
		}
	})

	t.Run("oidc_callback", func(t *testing.T) {
		stub := newStubOIDCWorkflowService(true)
		stub.authURL = "https://id.example.com/authorize"
		stub.result = services.OIDCLoginResult{
			User: models.User{
				ID:                 53,
				Role:               models.RoleOwner,
				AuthSessionVersion: 1,
				LocalAuthEnabled:   true,
				PasswordHash:       "$2a$10$0123456789abcdef01234uVwxyzABCD0123456789abcdef01234",
				MustChangePassword: true,
			},
			RequiresPasswordReset: true,
			ResetCause:            services.ResetCauseMustChangePassword,
		}
		app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
			cookieSecure:    true,
			oidcService:     stub,
			auditLogEnabled: true,
		})
		startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
		stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
		if stateCookie == nil {
			t.Fatal("expected OIDC state cookie from start flow")
		}

		request := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
			"state": {stub.lastStartState},
			"code":  {"provider-code"},
		}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Cookie", stateCookie.String())
		response, logOutput := captureAuditedRequest(t, app, request)
		assertStatusCode(t, response, fiber.StatusSeeOther)
		if line := securityEventLine(t, logOutput, "auth.oidc_callback", "reset_required"); !strings.Contains(line, "cause") || !strings.Contains(line, services.ResetCauseMustChangePassword) {
			t.Fatalf("expected cause %q on the reset_required line, got %q", services.ResetCauseMustChangePassword, line)
		}
	})
}
