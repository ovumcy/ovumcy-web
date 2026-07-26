package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// --- helpers ---

// sealTOTPSetupCookieForTest seals a pending-enrollment payload attributed to
// userID. Passing 0 produces the unattributed payload the writer now refuses to
// mint, which is how the read-side guard is exercised on its own.
func sealTOTPSetupCookieForTest(t *testing.T, secretKey []byte, userID uint, rawSecret string) string {
	t.Helper()
	payload := totpSetupCookiePayload{
		UserID:    userID,
		RawSecret: rawSecret,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal totp setup payload: %v", err)
	}
	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	sealed, err := codec.seal(totpSetupCookieName, serialized)
	if err != nil {
		t.Fatalf("seal totp setup: %v", err)
	}
	return totpSetupCookieName + "=" + sealed
}

func newTOTPSettingsContext(t *testing.T, email string) settingsSecurityTestContext {
	t.Helper()
	// Use the standard settings context; TOTP state is managed per-test via DB.
	return newSettingsSecurityTestContext(t, email)
}

func getTOTPServiceForTest(database *gorm.DB) *services.TOTPService {
	return services.NewTOTPService(&dbUserRepoForTest{database}, []byte("test-secret-key"), nil)
}

// --- ShowTOTPSetupPage ---

func TestShowTOTPSetupPage_Unauthenticated_RedirectsToLogin(t *testing.T) {
	app, _ := newOnboardingTestAppWithCSRF(t)

	req := httptest.NewRequest(http.MethodGet, "/settings/2fa", nil)
	req.Header.Set("Accept-Language", "en")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /settings/2fa: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", resp.StatusCode)
	}
}

func TestShowTOTPSetupPage_TOTPNotEnabled_RendersQRAndSecret(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-setup-page@example.com")

	req := httptest.NewRequest(http.MethodGet, "/settings/2fa", nil)
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", ctx.authCookie)
	resp, err := ctx.app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /settings/2fa: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "data:image/png;base64,") {
		t.Error("setup page should contain an inline QR code PNG")
	}
	if !strings.Contains(string(body), "data-totp-manual-secret") {
		t.Error("setup page should expose the manual TOTP secret element for e2e tests")
	}
}

func TestShowTOTPSetupPage_TOTPEnabled_ShowsManagementView(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-setup-enabled@example.com")
	svc := getTOTPServiceForTest(ctx.database)
	if err := svc.EnableTOTP(context.Background(), ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	// EnableTOTP bumps auth_session_version atomically, so the pre-enable
	// cookie is now stale. Refresh it to mirror what the real handler does
	// when the user toggles 2FA from their own session.
	ctx.refreshAuthCookie(t)

	req := httptest.NewRequest(http.MethodGet, "/settings/2fa", nil)
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", ctx.authCookie)
	resp, err := ctx.app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /settings/2fa: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	// Management view: must NOT show QR (negative) and MUST show the disable
	// form action + password field (positive). A blank page would otherwise
	// satisfy the QR-absence check.
	if strings.Contains(string(body), "data:image/png;base64,") {
		t.Error("management view should not show QR code when TOTP is already enabled")
	}
	if !strings.Contains(string(body), "/api/v1/users/current/2fa") {
		t.Error("management view should contain the disable form action")
	}
	if !strings.Contains(string(body), `name="password"`) {
		t.Error("management view should contain the password confirmation field")
	}
}

// --- VerifyTOTP2FAEnrollment ---

func TestVerifyTOTP2FAEnrollment_ValidCode_EnablesTOTP(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-valid@example.com")

	// Generate a real key and seal its secret in a setup cookie.
	key, err := services.NewTOTPService(&dbUserRepoForTest{ctx.database}, []byte("test-secret-key"), nil).GenerateSetupKey("Ovumcy", ctx.user.Email)
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	setupCookie := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), ctx.user.ID, key.Secret())

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	form := url.Values{"code": {code}, "password": {"StrongPass1"}}
	cloned := cloneFormValues(form)
	cloned.Set("csrf_token", ctx.csrfToken)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/current/2fa", strings.NewReader(cloned.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))
	resp, err := ctx.app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST /api/v1/users/current/2fa: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 200 or 303", resp.StatusCode)
	}
	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !reloaded.TOTPEnabled {
		t.Error("TOTPEnabled should be true after successful enrollment")
	}
	if reloaded.TOTPSecret == "" {
		t.Error("TOTPSecret should be set after enrollment")
	}
}

func TestVerifyTOTP2FAEnrollment_InvalidCode_DoesNotEnable(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-invalid@example.com")

	key, err := services.NewTOTPService(&dbUserRepoForTest{ctx.database}, []byte("test-secret-key"), nil).GenerateSetupKey("Ovumcy", ctx.user.Email)
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	setupCookie := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), ctx.user.ID, key.Secret())

	form := url.Values{"code": {"000000"}, "password": {"StrongPass1"}}
	cloned := cloneFormValues(form)
	cloned.Set("csrf_token", ctx.csrfToken)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/current/2fa", strings.NewReader(cloned.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))
	resp, err := ctx.app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST /api/v1/users/current/2fa: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.TOTPEnabled {
		t.Error("TOTPEnabled should be false after invalid code (unless 000000 was coincidentally valid)")
	}

	// TOTPEnabled==false is satisfied by any refusal, including ones that never reach
	// the code check: with a corrupted csrf_token this assertion held while the
	// request died in middleware. Pin the status so the test proves enrollment
	// verification ran and rejected the code.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 from the enrollment code check, got %d — the request may have been refused before verification", resp.StatusCode)
	}
}

func TestVerifyTOTP2FAEnrollment_MissingSetupCookie_ReturnsError(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-nocookie@example.com")

	form := url.Values{"code": {"123456"}, "password": {"StrongPass1"}}
	cloned := cloneFormValues(form)
	cloned.Set("csrf_token", ctx.csrfToken)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/current/2fa", strings.NewReader(cloned.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	resp, err := ctx.app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST /api/v1/users/current/2fa: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Without a setup cookie the handler maps to totpSessionExpiredErrorSpec
	// (401). /api/v1/users/current/2fa is not in the auth-redirect path, so the
	// status surfaces directly via apiError.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (totp session expired)", resp.StatusCode)
	}

	// Verify TOTP was NOT enabled despite the request reaching the handler.
	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.TOTPEnabled {
		t.Error("missing setup cookie must not enable TOTP")
	}
}

// totpEnrollmentRequest submits the enrollment confirmation for the session in
// ctx, carrying an explicitly supplied setup cookie so a test can present a
// payload the session did not mint.
func totpEnrollmentRequest(t *testing.T, ctx settingsSecurityTestContext, code string, setupCookie string) *http.Response {
	t.Helper()

	form := url.Values{"code": {code}, "password": {"StrongPass1"}}
	cloned := cloneFormValues(form)
	cloned.Set("csrf_token", ctx.csrfToken)

	request := httptest.NewRequest(http.MethodPut, "/api/v1/users/current/2fa", strings.NewReader(cloned.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("enrollment request failed: %v", err)
	}
	return response
}

// signInSecondOwner opens a second independent owner's authenticated session on
// the same instance — the household self-hosting shape, where two owners share
// one deployment and nothing else.
func signInSecondOwner(t *testing.T, host settingsSecurityTestContext, email string) settingsSecurityTestContext {
	t.Helper()

	user := createOnboardingTestUser(t, host.database, email, "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, host.app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, host.app, authCookie)

	return settingsSecurityTestContext{
		app:        host.app,
		database:   host.database,
		user:       user,
		authCookie: authCookie,
		csrfCookie: csrfCookie,
		csrfToken:  csrfToken,
	}
}

func totpStateForUser(t *testing.T, ctx settingsSecurityTestContext, userID uint) models.User {
	t.Helper()

	var reloaded models.User
	if err := ctx.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded
}

// TestVerifyTOTP2FAEnrollmentRefusesSetupCookieNotMintedForThisOwner pins the
// owner binding on the sealed enrollment cookie at the handler boundary.
//
// The cookie carries the RAW TOTP secret across the enrollment form submission.
// Without an owner id sealed into it, a setup payload minted for one owner and
// presented on another owner's session would enrol the first owner's pending
// secret as the second owner's own credential — credential confusion on a
// household instance, where two owners share one deployment. So the confirm step
// refuses a payload that names a different account, and refuses one that names
// no account at all rather than treating the missing id as a comparison that
// does not apply.
//
// Both payloads below are sealed under the app's own secret and carry a code
// that genuinely validates, and the presenting session supplies its own correct
// password, so the owner check is the only thing left between the request and
// enrollment. The positive anchor at the end proves enrollment still completes
// for the owner the payload was minted for, so the refusals above cannot be
// satisfied by an endpoint that enrols nobody.
func TestVerifyTOTP2FAEnrollmentRefusesSetupCookieNotMintedForThisOwner(t *testing.T) {
	ownerA := newTOTPSettingsContext(t, "totp-enroll-scope-a@example.com")
	ownerB := signInSecondOwner(t, ownerA, "totp-enroll-scope-b@example.com")

	key, err := getTOTPServiceForTest(ownerA.database).GenerateSetupKey("Ovumcy", ownerA.user.Email)
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	freshCode := func() string {
		t.Helper()
		code, codeErr := totp.GenerateCode(key.Secret(), time.Now())
		if codeErr != nil {
			t.Fatalf("GenerateCode: %v", codeErr)
		}
		return code
	}

	// Owner A's pending secret, presented on owner B's session.
	crossOwner := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), ownerA.user.ID, key.Secret())
	crossResponse := totpEnrollmentRequest(t, ownerB, freshCode(), crossOwner)
	defer func() { _ = crossResponse.Body.Close() }()

	if crossResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected a setup cookie minted for another owner to be refused with 401, got %d", crossResponse.StatusCode)
	}
	if cleared := responseCookie(crossResponse.Cookies(), totpSetupCookieName); cleared == nil || cleared.Value != "" {
		t.Fatal("expected the refused setup cookie to be cleared, not left presentable on a retry")
	}
	if state := totpStateForUser(t, ownerB, ownerB.user.ID); state.TOTPEnabled || state.TOTPSecret != "" {
		t.Fatal("another owner's pending secret must not be enrolled as this owner's credential")
	}
	if state := totpStateForUser(t, ownerA, ownerA.user.ID); state.TOTPEnabled || state.TOTPSecret != "" {
		t.Fatal("a refused cross-owner enrollment must not enable TOTP on the account the secret belongs to either")
	}

	// The same secret in a payload that names no owner at all.
	unattributed := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), 0, key.Secret())
	unattributedResponse := totpEnrollmentRequest(t, ownerB, freshCode(), unattributed)
	defer func() { _ = unattributedResponse.Body.Close() }()

	if unattributedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected an unattributed setup cookie to be refused with 401, got %d", unattributedResponse.StatusCode)
	}
	if cleared := responseCookie(unattributedResponse.Cookies(), totpSetupCookieName); cleared == nil || cleared.Value != "" {
		t.Fatal("expected the refused unattributed setup cookie to be cleared")
	}
	if state := totpStateForUser(t, ownerB, ownerB.user.ID); state.TOTPEnabled || state.TOTPSecret != "" {
		t.Fatal("an unattributed pending secret must not be enrolled against whichever session presented it")
	}

	// Positive anchor: the owner the payload was minted for enrols normally.
	ownedResponse := totpEnrollmentRequest(t, ownerA, freshCode(), crossOwner)
	defer func() { _ = ownedResponse.Body.Close() }()

	if ownedResponse.StatusCode != http.StatusOK && ownedResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("owner A must still complete its own enrollment, got %d", ownedResponse.StatusCode)
	}
	enrolled := totpStateForUser(t, ownerA, ownerA.user.ID)
	if !enrolled.TOTPEnabled || enrolled.TOTPSecret == "" {
		t.Fatal("owner A's own enrollment must persist the encrypted secret and enable TOTP")
	}
	if state := totpStateForUser(t, ownerB, ownerB.user.ID); state.TOTPEnabled || state.TOTPSecret != "" {
		t.Fatal("owner A completing its own enrollment must leave owner B untouched")
	}
}

// --- DisableTOTP2FA ---

func TestDisableTOTP2FA_CorrectPassword_DisablesTOTP(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-correct@example.com")
	svc := getTOTPServiceForTest(ctx.database)
	if err := svc.EnableTOTP(context.Background(), ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	// EnableTOTP bumped auth_session_version, so refresh the cookie before
	// exercising the disable endpoint.
	ctx.refreshAuthCookie(t)

	form := url.Values{"password": {"StrongPass1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/2fa", form, map[string]string{"Accept-Language": "en"})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 200 or 303", resp.StatusCode)
	}

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.TOTPEnabled {
		t.Error("TOTPEnabled should be false after disabling")
	}
	if reloaded.TOTPSecret != "" {
		t.Error("TOTPSecret should be cleared after disabling")
	}
}

func TestDisableTOTP2FA_WrongPassword_ReturnsError(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-wrong@example.com")
	svc := getTOTPServiceForTest(ctx.database)
	if err := svc.EnableTOTP(context.Background(), ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	// EnableTOTP bumped auth_session_version; without a refreshed cookie the
	// request dies at AuthRequired and the test goes green without ever
	// exercising the handler (the bug this fix removes).
	ctx.refreshAuthCookie(t)

	form := url.Values{"password": {"WrongPassword1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/2fa", form, map[string]string{"Accept-Language": "en", "Accept": "application/json"})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password: status = %d, want 401", resp.StatusCode)
	}

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !reloaded.TOTPEnabled {
		t.Error("TOTPEnabled should remain true after wrong password")
	}
}

// TestDisableTOTP2FA_RateLimited_AfterRepeatedWrongPassword guards against an
// authenticated session-stealing attacker brute-forcing the password to disable
// 2FA. After DefaultTOTPDisableAttemptsLimit failures, a subsequent attempt
// (even with the correct password) must be rejected by the rate limiter.
func TestDisableTOTP2FA_RateLimited_AfterRepeatedWrongPassword(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-rl@example.com")
	svc := getTOTPServiceForTest(ctx.database)
	if err := svc.EnableTOTP(context.Background(), ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}

	// EnableTOTP bumped auth_session_version; refresh so the requests reach
	// the handler instead of dying at AuthRequired (which previously made
	// this test pass without the limiter ever firing).
	ctx.refreshAuthCookie(t)

	wrongForm := url.Values{"password": {"WrongPassword1"}}
	for attempt := range services.DefaultTOTPDisableAttemptsLimit {
		resp := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/2fa", wrongForm, map[string]string{"Accept-Language": "en", "Accept": "application/json"})
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("attempt %d: status = %d, want 401 or 429", attempt, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// Even the correct password must be rejected once the limiter has tripped.
	correctForm := url.Values{"password": {"StrongPass1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/2fa", correctForm, map[string]string{"Accept-Language": "en", "Accept": "application/json"})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("rate-limited disable: status = %d, want 429", resp.StatusCode)
	}

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !reloaded.TOTPEnabled {
		t.Error("rate-limited disable request must not actually disable TOTP")
	}
}
