package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestVerifyTOTP2FAEnrollment_BumpsSessionVersion captures the contract that
// enabling 2FA invalidates every auth cookie issued before the toggle. The
// originating session is refreshed inline by the handler so the user that
// turned 2FA on stays signed in on their current device, while any other
// device carrying a cookie minted before the toggle is signed out on its
// next request.
func TestVerifyTOTP2FAEnrollment_BumpsSessionVersion(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enable-bumps-sv@example.com")
	otherSessionCookie := ctx.authCookie

	key, err := getTOTPServiceForTest(ctx.database).GenerateSetupKey("Ovumcy", ctx.user.Email)
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	setupCookie := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), key.Secret())

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	form := url.Values{"code": {code}, "password": {"StrongPass1"}, "csrf_token": {ctx.csrfToken}}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/current/2fa", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))
	resp, err := ctx.app.Test(req, -1)
	if err != nil {
		t.Fatalf("verify enroll: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("verify enroll status = %d, want 200 or 303", resp.StatusCode)
	}

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.AuthSessionVersion <= ctx.user.AuthSessionVersion {
		t.Fatalf("auth_session_version did not advance after enable: before=%d after=%d", ctx.user.AuthSessionVersion, reloaded.AuthSessionVersion)
	}

	refreshed := responseCookie(resp.Cookies(), authCookieName)
	if refreshed == nil || strings.TrimSpace(refreshed.Value) == "" {
		t.Fatal("enable handler must reissue ovumcy_auth so the originating session stays alive")
	}

	otherProbe := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	otherProbe.Header.Set("Cookie", otherSessionCookie)
	otherResp, err := ctx.app.Test(otherProbe, -1)
	if err != nil {
		t.Fatalf("other-session probe: %v", err)
	}
	defer otherResp.Body.Close()
	if otherResp.StatusCode == http.StatusOK {
		t.Fatalf("pre-toggle cookie still accepted on /dashboard (status=%d); session-version bump did not invalidate it", otherResp.StatusCode)
	}
}

// TestDisableTOTP2FA_BumpsSessionVersion captures the matching contract for
// the disable side. Same shape: disabling 2FA must invalidate cookies that
// were issued while 2FA was on, even though disabling is itself password-
// gated.
func TestDisableTOTP2FA_BumpsSessionVersion(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-bumps-sv@example.com")
	if err := getTOTPServiceForTest(ctx.database).EnableTOTP(context.Background(), ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP setup: %v", err)
	}
	ctx.refreshAuthCookie(t)
	preDisableCookie := ctx.authCookie
	preDisableVersion := ctx.user.AuthSessionVersion

	form := url.Values{"password": {"StrongPass1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/2fa", form, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("disable status = %d, want 200 or 303", resp.StatusCode)
	}

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.AuthSessionVersion <= preDisableVersion {
		t.Fatalf("auth_session_version did not advance after disable: before=%d after=%d", preDisableVersion, reloaded.AuthSessionVersion)
	}

	refreshed := responseCookie(resp.Cookies(), authCookieName)
	if refreshed == nil || strings.TrimSpace(refreshed.Value) == "" {
		t.Fatal("disable handler must reissue ovumcy_auth so the originating session stays alive")
	}

	otherProbe := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	otherProbe.Header.Set("Cookie", preDisableCookie)
	otherResp, err := ctx.app.Test(otherProbe, -1)
	if err != nil {
		t.Fatalf("other-session probe: %v", err)
	}
	defer otherResp.Body.Close()
	if otherResp.StatusCode == http.StatusOK {
		t.Fatalf("pre-toggle cookie still accepted on /dashboard (status=%d); session-version bump did not invalidate it", otherResp.StatusCode)
	}
}
