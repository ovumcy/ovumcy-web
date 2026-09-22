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

// TestPasswordResetRedeemBrowserSurfaceRedirectsToRecoveryCode pins the claim
// docs/openapi.yaml makes for POST /api/v1/password-resets/redeem's `303`
// response: the browser surface lands on `/recovery-code`, not `/login`. The
// handler answers through the same renderRecoveryCodeResponse/redirectToPath
// path the JSON body's `next_path` names — see
// TestResetPasswordJSONSuccessDoesNotExposeRecoveryCode for that half — so a
// spec that kept naming `/login` here would mislead every non-JSON client
// into polling the wrong page for the one-time code.
func TestPasswordResetRedeemBrowserSurfaceRedirectsToRecoveryCode(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "redeem-browser-redirect@example.com", "StrongPass1", true)
	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookie := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

	form := url.Values{
		"password":         {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Deliberately no Accept: application/json — this is the browser surface
	// the spec's 303 response describes.
	request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/recovery-code" {
		t.Fatalf("Location = %q, want /recovery-code (docs/openapi.yaml's own claim for this response)", location)
	}
	if cookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName); cookie == "" {
		t.Fatal("expected a sealed recovery-code reveal cookie alongside the redirect")
	}
}

// TestForcedResetFromOIDCUnverifiableTOTPRedeemCompletesRecovery is the redeem
// half of TestOIDCCallbackRoutesRequiresPasswordResetToResetEvenWithoutMustChangePassword,
// which only proves the callback mints the forced-reset cookie for this
// reason. docs/openapi.yaml's 403 description for POST
// /api/v1/password-resets/redeem now names TWO reasons a forced-from-OIDC
// token survives the local-auth-disabled gate — an operator-set
// must_change_password AND an enrolled-but-unverifiable TOTP secret — and
// claims the account recovery path stays unbroken either way. This proves
// the second reason all the way through: the token redeems, a session is
// issued, and a fresh recovery code is minted, exactly like the
// must_change_password case TestForcedResetFromOIDCRedeemSurvivesLocalPublicAuthBeingOff
// already covers.
func TestForcedResetFromOIDCUnverifiableTOTPRedeemCompletesRecovery(t *testing.T) {
	app, database, stub := newLocalAuthGateTestApp(t)
	user := createOnboardingTestUser(t, database, "forced-oidc-unverifiable-totp@example.com", "StrongPass1", true)
	if user.MustChangePassword {
		t.Fatalf("fixture invariant broken: MustChangePassword=%v", user.MustChangePassword)
	}

	// The stub bypasses OIDCLoginService.Authenticate's own computation, so
	// RequiresPasswordReset is set here exactly as the real service derives it
	// for an account whose TOTP secret has gone unverifiable (SECRET_KEY
	// rotation) — TOTPEnabled=true, MustChangePassword=false, the shape
	// oidc_login_service.go's own RequiresPasswordReset expression produces for
	// that reason.
	resetCookie := forcedOIDCResetCookieFromUnverifiableTOTPCallback(t, app, stub, user)

	response := redeemResetCookie(t, app, resetCookie, "EvenStronger2")
	assertStatusCode(t, response, http.StatusOK)

	payload := readRecoveryCodeFlowJSON(t, response)
	assertRecoveryCodeIssuedViaSurface(t, payload, "/recovery-code")
	assertRecoveryCodeTransportCookies(t, response)
}

// forcedOIDCResetCookieFromUnverifiableTOTPCallback is
// forcedOIDCResetCookieFromCallback's sibling for the OTHER reason a forced
// reset reaches CompleteOIDCLogin: RequiresPasswordReset=true with
// MustChangePassword=false is exactly what an enrolled-but-unverifiable TOTP
// secret produces (oidc_login_service.go), so the stub is set directly
// rather than derived from the persisted user's MustChangePassword flag.
func forcedOIDCResetCookieFromUnverifiableTOTPCallback(t *testing.T, app *fiber.App, stub *stubOIDCWorkflowService, user models.User) string {
	t.Helper()

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	stub.result = services.OIDCLoginResult{
		User:                  user,
		RequiresPasswordReset: true,
		RequiresTOTP:          false,
	}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected the OIDC callback to route the forced-reset user to /reset-password, got %q", location)
	}

	cookieValue := responseCookieValue(callbackResponse.Cookies(), resetPasswordCookieName)
	if cookieValue == "" {
		t.Fatal("expected the OIDC callback to seal a reset-password cookie for a forced-reset user")
	}
	return cookieValue
}
