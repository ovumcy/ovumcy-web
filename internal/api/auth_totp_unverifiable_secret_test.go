package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
)

// TestLoginRoutesUnverifiableTOTPToForcedResetWithoutMustChangePassword is the
// API-level proof this change exists for: an account enrolled in TOTP whose
// stored secret cannot be decrypted under the instance's active SECRET_KEY —
// simulating a SECRET_KEY rotation — but carrying NO operator-set
// must_change_password must still land on /reset-password, exactly like an
// operator-flagged account, rather than either:
//   - a permanent lockout at /auth/2fa (a 2FA-pending cookie for a challenge
//     no code could ever satisfy), or
//   - a silent bypass straight to a live session.
//
// Before this change, LoginService.Authenticate read the raw TOTPEnabled
// column: this account would have received RequiresTOTP=true and been sent
// to a 2FA challenge it could never pass.
func TestLoginRoutesUnverifiableTOTPToForcedResetWithoutMustChangePassword(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "totp-unverifiable-login@example.com", "StrongPass1", true)
	setupTOTPForUser(t, database, user.ID, []byte(testAppSecretKey))

	// Simulate a SECRET_KEY rotation: overwrite the stored ciphertext with a
	// value sealed under a key this instance does not hold. totp_enabled is
	// left untouched — the account is still enrolled, just unverifiable.
	rotated, err := security.EncryptField("does-not-matter", []byte("a-completely-different-secret-k"), []byte("irrelevant-aad"))
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("totp_secret", rotated).Error; err != nil {
		t.Fatalf("corrupt totp_secret: %v", err)
	}
	var beforeLogin models.User
	if err := database.First(&beforeLogin, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !beforeLogin.TOTPEnabled || beforeLogin.MustChangePassword {
		t.Fatalf("fixture invariant broken: TOTPEnabled=%v MustChangePassword=%v", beforeLogin.TOTPEnabled, beforeLogin.MustChangePassword)
	}

	form := url.Values{
		"email":    {user.Email},
		"password": {"StrongPass1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST /api/v1/sessions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/reset-password" {
		t.Fatalf("Location = %q, want /reset-password (an unverifiable TOTP secret must reach the forced-reset escape hatch)", loc)
	}
	if c := responseCookie(resp.Cookies(), authCookieName); c != nil && strings.TrimSpace(c.Value) != "" {
		t.Error("did not expect an auth cookie — that would be a second-factor bypass")
	}
	if c := responseCookie(resp.Cookies(), totpPendingCookieName); c != nil && strings.TrimSpace(c.Value) != "" {
		t.Error("did not expect a TOTP pending cookie — that challenge could never be satisfied, which is the lockout this change removes")
	}
	resetCookie := responseCookie(resp.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatal("expected a reset-password cookie on the forced-reset escape hatch")
	}
}

// TestCompleteOIDCLinkConfirmationRoutesUnverifiableTOTPToResetWithoutASession
// is the link-confirm sibling of the test above, pinning the THIRD, separately
// -reasoned TOTP consumer this change touches: CompleteOIDCLinkConfirmation
// used to gate on the raw targetUser.TOTPEnabled column directly, demanding a
// totp_code in the same submission whenever it was true — regardless of
// whether the stored secret could ever be decrypted. An account whose secret
// is unverifiable (SECRET_KEY rotation) and carries no must_change_password
// flag must reach /reset-password (via LoginService.Authenticate's own
// forced-reset result, computed a few lines above the TOTP gate in the
// handler) WITHOUT the submission ever carrying a valid totp_code — there is
// no code that could ever be valid for this account.
func TestCompleteOIDCLinkConfirmationRoutesUnverifiableTOTPToResetWithoutASession(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	user := createOnboardingTestUser(t, database, "link-unverifiable-totp@example.com", "StrongPass1", true)
	setupTOTPForUser(t, database, user.ID, []byte(testHandlerSecretKey))

	rotated, err := security.EncryptField("does-not-matter", []byte("a-completely-different-secret-k"), []byte("irrelevant-aad"))
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("totp_secret", rotated).Error; err != nil {
		t.Fatalf("corrupt totp_secret: %v", err)
	}

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-unverifiable-totp", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	// No totp_code field at all — there is no valid code for this account, so
	// the submission does not attempt one. The old TOTPEnabled-only gate would
	// have refused this as a missing/invalid code and kept the user stuck on
	// the link-confirm form forever.
	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected redirect to /reset-password for an unverifiable-TOTP target, got %q", location)
	}
	resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatal("expected a reset-password cookie on the forced-reset recovery path")
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect an auth cookie for an unverifiable-TOTP target — that would be a second-factor bypass")
	}
}

// TestOIDCCallbackRoutesRequiresPasswordResetToResetEvenWithoutMustChangePassword
// isolates the handler's own consumption of OIDCLoginResult from
// OIDCLoginService.Authenticate's computation of it (already pinned at the
// service layer by TestOIDCLoginServiceAuthenticateRoutesUnverifiableTOTPToForcedReset).
// The stub result below carries RequiresPasswordReset=true with
// User.MustChangePassword=false — exactly the shape an unverifiable-TOTP
// account produces — so this proves CompleteOIDCLogin branches on the
// derived field, not on the raw routing flag it used to read directly.
func TestOIDCCallbackRoutesRequiresPasswordResetToResetEvenWithoutMustChangePassword(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.result = services.OIDCLoginResult{
		User: models.User{
			ID:                 51,
			Role:               models.RoleOwner,
			AuthSessionVersion: 1,
			PasswordHash:       "$2a$10$0123456789abcdef01234uVwxyzABCD0123456789abcdef01234",
			TOTPEnabled:        true,
			MustChangePassword: false,
		},
		RequiresPasswordReset: true,
	}
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
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

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, fiber.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected redirect to /reset-password, got %q", location)
	}
	if c := responseCookie(callbackResponse.Cookies(), totpPendingCookieName); c != nil && strings.TrimSpace(c.Value) != "" {
		t.Fatal("did not expect a TOTP pending cookie when RequiresPasswordReset is set")
	}
	if authCookie := responseCookie(callbackResponse.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect an auth cookie — RequiresPasswordReset must route to reset, not straight through")
	}
	resetCookie := responseCookie(callbackResponse.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatal("expected a reset-password cookie")
	}
}

// TestVerifyTOTPLoginRefusesAPendingCookieForAnAccountThatBecameUnverifiable
// covers the narrow race the challenge handler itself must not fall into:
// a pending-TOTP cookie minted while the account's secret was still
// verifiable, followed by the secret becoming unverifiable (a SECRET_KEY
// rotation) before the challenge is submitted. Sent with Accept:
// application/json so the response carries the mapped spec's own status and
// key rather than the uniform 303 the HTML form path answers every 2FA
// refusal with: VerifyTOTPLogin must answer with totpSessionExpiredErrorSpec
// (401, "totp session expired") — the same shape a stale/invalid pending
// cookie gets — rather than falling through to ValidateCode, which would
// answer with totpInternalErrorSpec (500, "totp internal error") regardless
// of the code submitted: an opaque internal error instead of a signal that
// points the owner toward the next login's forced-reset escape hatch.
func TestVerifyTOTPLoginRefusesAPendingCookieForAnAccountThatBecameUnverifiable(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "totp-became-unverifiable@example.com", "StrongPass1", true)
	secretKey := []byte(testHandlerSecretKey)
	rawSecret := setupTOTPForUser(t, database, user.ID, secretKey)
	pendingCookie := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)

	code, err := totp.GenerateCode(rawSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	// Simulate a SECRET_KEY rotation happening after the pending cookie was
	// minted but before the challenge is submitted.
	rotated, err := security.EncryptField("does-not-matter", []byte("a-completely-different-secret-k"), []byte("irrelevant-aad"))
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("totp_secret", rotated).Error; err != nil {
		t.Fatalf("corrupt totp_secret: %v", err)
	}

	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)
	form := url.Values{"code": {code}, "csrf_token": {csrfToken}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", joinCookieHeader(pendingCookie, csrfCookieHeader))
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST /api/v1/sessions/2fa-challenge: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (totpSessionExpiredErrorSpec, not an internal error or a successful login)", resp.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	decodeJSONBody(t, resp.Body, &body)
	if body.Error != "totp session expired" {
		t.Fatalf("error = %q, want %q", body.Error, "totp session expired")
	}
	if authCookie := responseCookie(resp.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatal("did not expect an auth cookie")
	}
}
