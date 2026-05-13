package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
)

func TestVerifyTOTP2FAEnrollment_MissingPassword_DoesNotEnable(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-missing-pass@example.com")

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
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))
	resp, err := ctx.app.Test(req, -1)
	if err != nil {
		t.Fatalf("POST /api/settings/2fa/verify: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing password)", resp.StatusCode)
	}
	if got := readAPIError(t, resp.Body); got != "invalid password" {
		t.Fatalf("expected error %q, got %q", "invalid password", got)
	}

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.TOTPEnabled {
		t.Error("missing password must not enable TOTP")
	}
}

func TestVerifyTOTP2FAEnrollment_WrongPassword_DoesNotEnable(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-wrong-pass@example.com")

	key, err := services.NewTOTPService(&dbUserRepoForTest{ctx.database}, []byte("test-secret-key"), nil).GenerateSetupKey("Ovumcy", ctx.user.Email)
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	setupCookie := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), key.Secret())

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	form := url.Values{"code": {code}, "password": {"NotMyPass99"}}
	cloned := cloneFormValues(form)
	cloned.Set("csrf_token", ctx.csrfToken)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/2fa/verify", strings.NewReader(cloned.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))
	resp, err := ctx.app.Test(req, -1)
	if err != nil {
		t.Fatalf("POST /api/settings/2fa/verify: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (wrong password)", resp.StatusCode)
	}
	if got := readAPIError(t, resp.Body); got != "invalid password" {
		t.Fatalf("expected error %q, got %q", "invalid password", got)
	}

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.TOTPEnabled {
		t.Error("wrong password must not enable TOTP")
	}
}
