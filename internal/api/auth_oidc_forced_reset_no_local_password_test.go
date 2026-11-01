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
