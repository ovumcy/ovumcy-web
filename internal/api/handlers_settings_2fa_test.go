package api

import (
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

func sealTOTPSetupCookieForTest(t *testing.T, secretKey []byte, rawSecret string) string {
	t.Helper()
	payload := totpSetupCookiePayload{
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

func sealExpiredTOTPSetupCookieForTest(t *testing.T, secretKey []byte, rawSecret string) string {
	t.Helper()
	payload := totpSetupCookiePayload{
		RawSecret: rawSecret,
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal expired totp setup payload: %v", err)
	}
	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	sealed, err := codec.seal(totpSetupCookieName, serialized)
	if err != nil {
		t.Fatalf("seal expired totp setup: %v", err)
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
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET /settings/2fa: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", resp.StatusCode)
	}
}

func TestShowTOTPSetupPage_TOTPNotEnabled_RendersQRAndSecret(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-setup-page@example.com")

	req := httptest.NewRequest(http.MethodGet, "/settings/2fa", nil)
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", ctx.authCookie)
	resp, err := ctx.app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET /settings/2fa: %v", err)
	}
	defer resp.Body.Close()

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
	if err := svc.EnableTOTP(ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/settings/2fa", nil)
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", ctx.authCookie)
	resp, err := ctx.app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET /settings/2fa: %v", err)
	}
	defer resp.Body.Close()

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
	if !strings.Contains(string(body), "/api/settings/2fa/disable") {
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
	setupCookie := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), key.Secret())

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	form := url.Values{"code": {code}}
	cloned := cloneFormValues(form)
	cloned.Set("csrf_token", ctx.csrfToken)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/2fa/verify", strings.NewReader(cloned.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))
	resp, err := ctx.app.Test(req, -1)
	if err != nil {
		t.Fatalf("POST /api/settings/2fa/verify: %v", err)
	}
	defer resp.Body.Close()

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
	setupCookie := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), key.Secret())

	form := url.Values{"code": {"000000"}}
	cloned := cloneFormValues(form)
	cloned.Set("csrf_token", ctx.csrfToken)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/2fa/verify", strings.NewReader(cloned.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))
	resp, err := ctx.app.Test(req, -1)
	if err != nil {
		t.Fatalf("POST /api/settings/2fa/verify: %v", err)
	}
	defer resp.Body.Close()

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.TOTPEnabled {
		t.Error("TOTPEnabled should be false after invalid code (unless 000000 was coincidentally valid)")
	}
}

func TestVerifyTOTP2FAEnrollment_MissingSetupCookie_ReturnsError(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-nocookie@example.com")

	form := url.Values{"code": {"123456"}}
	cloned := cloneFormValues(form)
	cloned.Set("csrf_token", ctx.csrfToken)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/2fa/verify", strings.NewReader(cloned.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	resp, err := ctx.app.Test(req, -1)
	if err != nil {
		t.Fatalf("POST /api/settings/2fa/verify: %v", err)
	}
	defer resp.Body.Close()

	// Without a setup cookie the handler maps to totpSessionExpiredErrorSpec
	// (401). /api/settings/2fa/verify is not in the auth-redirect path, so the
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

// --- DisableTOTP2FA ---

func TestDisableTOTP2FA_CorrectPassword_DisablesTOTP(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-correct@example.com")
	svc := getTOTPServiceForTest(ctx.database)
	if err := svc.EnableTOTP(ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}

	form := url.Values{"password": {"StrongPass1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/2fa/disable", form, map[string]string{"Accept-Language": "en"})
	defer resp.Body.Close()

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
	if err := svc.EnableTOTP(ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}

	form := url.Values{"password": {"WrongPassword1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/2fa/disable", form, map[string]string{"Accept-Language": "en"})
	defer resp.Body.Close()

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
	if err := svc.EnableTOTP(ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}

	wrongForm := url.Values{"password": {"WrongPassword1"}}
	for attempt := 0; attempt < services.DefaultTOTPDisableAttemptsLimit; attempt++ {
		resp := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/2fa/disable", wrongForm, map[string]string{"Accept-Language": "en"})
		resp.Body.Close()
	}

	// Even the correct password must be rejected once the limiter has tripped.
	correctForm := url.Values{"password": {"StrongPass1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/2fa/disable", correctForm, map[string]string{"Accept-Language": "en"})
	defer resp.Body.Close()

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !reloaded.TOTPEnabled {
		t.Error("rate-limited disable request must not actually disable TOTP")
	}
}
