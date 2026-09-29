package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/pquerna/otp/totp"
)

// twoFARequest describes one PUT/DELETE /api/v1/users/current/2fa call. The
// body and the query are set independently so a test can put a value in one
// and not the other.
type twoFARequest struct {
	method       string
	query        string
	contentType  string
	body         string
	setupCookie  string
	withSession  bool
	withCSRFHead bool
}

func send2FARequest(t *testing.T, ctx settingsSecurityTestContext, spec twoFARequest) *http.Response {
	t.Helper()
	target := "/api/v1/users/current/2fa"
	if spec.query != "" {
		target += "?" + spec.query
	}
	req := httptest.NewRequest(spec.method, target, strings.NewReader(spec.body))
	req.Header.Set("Content-Type", spec.contentType)
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Accept", "application/json")
	cookies := []string{cookiePair(ctx.csrfCookie), spec.setupCookie}
	if spec.withSession {
		cookies = append([]string{ctx.authCookie}, cookies...)
	}
	req.Header.Set("Cookie", joinCookieHeader(cookies...))
	if spec.withCSRFHead {
		req.Header.Set("X-CSRF-Token", ctx.csrfToken)
	}
	resp, err := ctx.app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s %s: %v", spec.method, target, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// assert2FARefusal checks the status and that the error envelope names the
// expected key, so two refusals sharing a status are told apart.
func assert2FARefusal(t *testing.T, resp *http.Response, wantStatus int, wantKey, label string) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s: read body: %v", label, err)
	}
	if resp.StatusCode != wantStatus {
		t.Errorf("%s: status = %d, want %d (body %s)", label, resp.StatusCode, wantStatus, body)
	}
	if !strings.Contains(string(body), wantKey) {
		t.Errorf("%s: body %s does not name %q", label, body, wantKey)
	}
}

func totpEnabledInDatabase(t *testing.T, ctx settingsSecurityTestContext) bool {
	t.Helper()
	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return reloaded.TOTPEnabled
}

// enrollmentFixture returns a setup cookie for a fresh secret, a code that
// validates against it, and a code proven not to.
func enrollmentFixture(t *testing.T, ctx settingsSecurityTestContext) (setupCookie, validCode, wrongCode string) {
	t.Helper()
	key, err := getTOTPServiceForTest(ctx.database).GenerateSetupKey("Ovumcy", ctx.user.Email)
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	return sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), ctx.user.ID, key.Secret()), code, invalidTOTPCodeForSkewWindow(t, key.Secret())
}

func enableTOTPForSettingsTest(t *testing.T, ctx *settingsSecurityTestContext) {
	t.Helper()
	if err := getTOTPServiceForTest(ctx.database).EnableTOTP(context.Background(), ctx.user.ID, ctx.user.AuthSessionVersion, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	ctx.refreshAuthCookie(t)
}

func jsonBodyFor(fields map[string]string) string {
	parts := make([]string, 0, len(fields))
	for key, value := range fields {
		parts = append(parts, `"`+key+`":"`+value+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// TestTOTPSettingsReadCodeAndPasswordFromTheBodyOnly pins both halves of the
// 2FA mutations' input contract: the code and the password come from the
// request body over either published transport, and a value that sits only in
// the URL query is not a submission.
func TestTOTPSettingsReadCodeAndPasswordFromTheBodyOnly(t *testing.T) {
	t.Run("PUT with the code only in the query is not enrolled", func(t *testing.T) {
		ctx := newTOTPSettingsContext(t, "totp-body-only-put-query@example.com")
		setupCookie, code, _ := enrollmentFixture(t, ctx)

		for _, transport := range []struct {
			name, contentType, body string
		}{
			{"form", "application/x-www-form-urlencoded", url.Values{"password": {"StrongPass1"}, "csrf_token": {ctx.csrfToken}}.Encode()},
			{"json", "application/json", jsonBodyFor(map[string]string{"password": "StrongPass1"})},
		} {
			resp := send2FARequest(t, ctx, twoFARequest{
				method: http.MethodPut, query: "code=" + code, contentType: transport.contentType,
				body: transport.body, setupCookie: setupCookie, withSession: true, withCSRFHead: true,
			})
			assert2FARefusal(t, resp, http.StatusUnauthorized, "totp invalid code", transport.name+" (a query code is no code)")
			if totpEnabledInDatabase(t, ctx) {
				t.Fatalf("%s: a code carried only in the query enrolled 2FA", transport.name)
			}
		}
	})

	t.Run("DELETE with the password only in the query is refused and 2FA stays on", func(t *testing.T) {
		ctx := newTOTPSettingsContext(t, "totp-body-only-delete-query@example.com")
		enableTOTPForSettingsTest(t, &ctx)

		for _, transport := range []struct {
			name, contentType, body string
		}{
			{"form", "application/x-www-form-urlencoded", url.Values{"csrf_token": {ctx.csrfToken}}.Encode()},
			{"json", "application/json", `{}`},
		} {
			resp := send2FARequest(t, ctx, twoFARequest{
				method: http.MethodDelete, query: "password=StrongPass1", contentType: transport.contentType,
				body: transport.body, withSession: true, withCSRFHead: true,
			})
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400 (a query password is no password)", transport.name, resp.StatusCode)
			}
			if !totpEnabledInDatabase(t, ctx) {
				t.Fatalf("%s: a password carried only in the query disabled 2FA", transport.name)
			}
		}
	})

	t.Run("a body member wins over a conflicting query member", func(t *testing.T) {
		ctx := newTOTPSettingsContext(t, "totp-body-only-conflict@example.com")
		enableTOTPForSettingsTest(t, &ctx)

		resp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodDelete, query: "password=StrongPass1", contentType: "application/json",
			body: jsonBodyFor(map[string]string{"password": "WrongPass9"}), withSession: true, withCSRFHead: true,
		})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401: the query password must not replace the body's", resp.StatusCode)
		}
		if !totpEnabledInDatabase(t, ctx) {
			t.Fatal("2FA was disabled by the query password")
		}
	})

	t.Run("PUT JSON with password and code enrolls", func(t *testing.T) {
		ctx := newTOTPSettingsContext(t, "totp-body-only-put-json@example.com")
		setupCookie, code, _ := enrollmentFixture(t, ctx)

		resp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodPut, contentType: "application/json",
			body:        jsonBodyFor(map[string]string{"password": "StrongPass1", "code": code}),
			setupCookie: setupCookie, withSession: true, withCSRFHead: true,
		})
		assert2FAOkEnvelope(t, ctx.app, resp)
		if !totpEnabledInDatabase(t, ctx) {
			t.Fatal("a JSON body with password and code did not enroll 2FA")
		}
	})

	t.Run("DELETE JSON with the password disables", func(t *testing.T) {
		ctx := newTOTPSettingsContext(t, "totp-body-only-delete-json@example.com")
		enableTOTPForSettingsTest(t, &ctx)

		resp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodDelete, contentType: "application/json",
			body: jsonBodyFor(map[string]string{"password": "StrongPass1"}), withSession: true, withCSRFHead: true,
		})
		assert2FAOkEnvelope(t, ctx.app, resp)
		if totpEnabledInDatabase(t, ctx) {
			t.Fatal("a JSON body with the password did not disable 2FA")
		}
	})

	t.Run("JSON refusals", func(t *testing.T) {
		ctx := newTOTPSettingsContext(t, "totp-body-only-refusals@example.com")
		setupCookie, code, wrongCode := enrollmentFixture(t, ctx)

		wrongCodeResp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodPut, contentType: "application/json",
			body:        jsonBodyFor(map[string]string{"password": "StrongPass1", "code": wrongCode}),
			setupCookie: setupCookie, withSession: true, withCSRFHead: true,
		})
		assert2FARefusal(t, wrongCodeResp, http.StatusUnauthorized, "totp invalid code", "wrong code")
		wrongPasswordResp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodPut, contentType: "application/json",
			body:        jsonBodyFor(map[string]string{"password": "WrongPass9", "code": code}),
			setupCookie: setupCookie, withSession: true, withCSRFHead: true,
		})
		if wrongPasswordResp.StatusCode != http.StatusUnauthorized {
			t.Errorf("wrong password: status = %d, want 401", wrongPasswordResp.StatusCode)
		}
		noCSRFResp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodPut, contentType: "application/json",
			body:        jsonBodyFor(map[string]string{"password": "StrongPass1", "code": code}),
			setupCookie: setupCookie, withSession: true,
		})
		if noCSRFResp.StatusCode != http.StatusForbidden {
			t.Errorf("no CSRF header: status = %d, want 403", noCSRFResp.StatusCode)
		}
		noSessionResp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodPut, contentType: "application/json",
			body:        jsonBodyFor(map[string]string{"password": "StrongPass1", "code": code}),
			setupCookie: setupCookie, withCSRFHead: true,
		})
		if noSessionResp.StatusCode != http.StatusUnauthorized {
			t.Errorf("no session: status = %d, want 401", noSessionResp.StatusCode)
		}
		if totpEnabledInDatabase(t, ctx) {
			t.Fatal("a refused request enrolled 2FA")
		}
	})

	t.Run("DELETE JSON refusals", func(t *testing.T) {
		ctx := newTOTPSettingsContext(t, "totp-body-only-delete-refusals@example.com")
		enableTOTPForSettingsTest(t, &ctx)

		body := jsonBodyFor(map[string]string{"password": "StrongPass1"})
		wrongPasswordResp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodDelete, contentType: "application/json",
			body: jsonBodyFor(map[string]string{"password": "WrongPass9"}), withSession: true, withCSRFHead: true,
		})
		if wrongPasswordResp.StatusCode != http.StatusUnauthorized {
			t.Errorf("wrong password: status = %d, want 401", wrongPasswordResp.StatusCode)
		}
		malformedResp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodDelete, contentType: "application/json",
			body: `{"password":`, withSession: true, withCSRFHead: true,
		})
		if malformedResp.StatusCode != http.StatusBadRequest {
			t.Errorf("malformed JSON: status = %d, want 400", malformedResp.StatusCode)
		}
		noCSRFResp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodDelete, contentType: "application/json", body: body, withSession: true,
		})
		if noCSRFResp.StatusCode != http.StatusForbidden {
			t.Errorf("no CSRF header: status = %d, want 403", noCSRFResp.StatusCode)
		}
		noSessionResp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodDelete, contentType: "application/json", body: body, withCSRFHead: true,
		})
		if noSessionResp.StatusCode != http.StatusUnauthorized {
			t.Errorf("no session: status = %d, want 401", noSessionResp.StatusCode)
		}
		if !totpEnabledInDatabase(t, ctx) {
			t.Fatal("a refused request disabled 2FA")
		}
	})
}
