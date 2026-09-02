package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// The reset flow has two kinds of token and only one of them belongs to local
// public auth. A RECOVERY-minted token is the redeem half of the flow
// ForgotPassword refuses to start once the operator switches local public auth
// off, so it must stop redeeming at the same moment: otherwise a token minted
// the minute before the switch still rewrites the password, still sets
// local_auth_enabled back to true, and still mints a session on an oidc_only
// instance. A FORCED token is the opposite — the OIDC callback and link-confirm
// paths mint one, both are live under oidc_only, and gating them would strand an
// owner carrying must_change_password.
//
// Both tests below mint their cookie while local public auth is still ON and
// flip the switch afterwards, which is the exact sequence the finding describes.

func TestRecoveryResetRedeemRefusesOnceLocalPublicAuthIsOff(t *testing.T) {
	app, database, stub := newLocalAuthGateTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-gate-recovery@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookieValue := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

	stub.localPublicAuthEnabled = false

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")
	assertStatusCode(t, response, http.StatusForbidden)
	if got := readAPIError(t, response.Body); got != "local recovery unavailable" {
		t.Fatalf("expected %q, got %q", "local recovery unavailable", got)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("expected a refused redeem to mint no session, got %#v", authCookie)
	}

	// The refusal is a refusal, not a delay: the account's posture is untouched.
	var stored models.User
	if err := database.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if stored.PasswordHash != user.PasswordHash {
		t.Fatal("expected the refused redeem to leave the password hash unchanged")
	}
	if stored.AuthSessionVersion != user.AuthSessionVersion {
		t.Fatalf("expected auth_session_version to stay %d, got %d", user.AuthSessionVersion, stored.AuthSessionVersion)
	}
}

func TestForcedResetRedeemSurvivesLocalPublicAuthBeingOff(t *testing.T) {
	app, database, stub := newLocalAuthGateTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-gate-forced@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}

	resetCookieValue := forcedResetCookieFromLogin(t, app, user.Email, "StrongPass1")

	stub.localPublicAuthEnabled = false

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")
	assertStatusCode(t, response, http.StatusOK)
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatalf("expected the forced redeem to mint a session, got %#v", authCookie)
	}
}

func TestResetPasswordPageFollowsTheRedeemGate(t *testing.T) {
	app, database, stub := newLocalAuthGateTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-gate-page@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	recoveryCookie := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

	forcedUser := createOnboardingTestUser(t, database, "reset-gate-page-forced@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", forcedUser.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}
	forcedCookie := forcedResetCookieFromLogin(t, app, forcedUser.Email, "StrongPass1")

	stub.localPublicAuthEnabled = false

	testCases := []struct {
		name       string
		cookie     string
		wantStatus int
	}{
		{name: "recovery token", cookie: recoveryCookie, wantStatus: http.StatusSeeOther},
		{name: "no token", cookie: "", wantStatus: http.StatusSeeOther},
		{name: "forced token", cookie: forcedCookie, wantStatus: http.StatusOK},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/reset-password", nil)
			if testCase.cookie != "" {
				request.Header.Set("Cookie", resetPasswordCookieName+"="+testCase.cookie)
			}
			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, testCase.wantStatus)
			if testCase.wantStatus == http.StatusSeeOther {
				if location := response.Header.Get("Location"); location != "/login" {
					t.Fatalf("expected redirect to /login, got %q", location)
				}
			}
			// A cookie the request never presented is never retracted: a bare
			// visit must not be answered with a Set-Cookie for a value nobody
			// sent, which is the rule the TOTP readers already follow.
			if testCase.cookie == "" {
				if retracted := responseCookie(response.Cookies(), resetPasswordCookieName); retracted != nil {
					t.Fatalf("expected no reset-password cookie header on a request that carried none, got %#v", retracted)
				}
			}
		})
	}
}

func newLocalAuthGateTestApp(t *testing.T) (*fiber.App, *gorm.DB, *stubOIDCWorkflowService) {
	t.Helper()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	return app, database, stub
}

func redeemResetCookie(t *testing.T, app *fiber.App, resetCookieValue string, password string) *http.Response {
	t.Helper()

	form := url.Values{
		"password":         {password},
		"confirm_password": {password},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)
	return mustAppResponse(t, app, request)
}

// forcedResetCookieFromLogin drives the real forced-reset path rather than
// sealing a cookie by hand, so the payload under test is the one production
// writes. The OIDC callback and link-confirm paths seal the identical payload.
func forcedResetCookieFromLogin(t *testing.T, app *fiber.App, email string, password string) string {
	t.Helper()

	form := url.Values{"email": {email}, "password": {password}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	cookieValue := responseCookieValue(response.Cookies(), resetPasswordCookieName)
	if cookieValue == "" {
		t.Fatal("expected the forced-reset login to seal a reset-password cookie")
	}
	return cookieValue
}
