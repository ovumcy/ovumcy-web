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
	"github.com/ovumcy/ovumcy-web/internal/services"
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

// TestVerifyTOTP2FAEnrollmentRefusesABodyItCouldNotDecodeWhole is the PUT
// counterpart: the password step binds the body first, so a body that reaches
// the code read has already decoded once. A second decode that fails — the code
// member given twice, the last of the wrong type, which leaves the valid first
// value in place — must not enroll with the code it left behind. The code is
// valid and the password correct in every case, so only the refusal of the body
// itself keeps 2FA off.
func TestVerifyTOTP2FAEnrollmentRefusesABodyItCouldNotDecodeWhole(t *testing.T) {
	cases := []struct {
		name, contentType string
		body              func(code string) string
		wantStatus        int
		wantKey           string
	}{
		{
			name: "a valid code followed by the same member of the wrong type", contentType: "application/json",
			body:       func(code string) string { return `{"password":"StrongPass1","code":"` + code + `","code":7}` },
			wantStatus: http.StatusUnauthorized, wantKey: "totp invalid code",
		},
		{
			name: "a code of the wrong type", contentType: "application/json",
			body:       func(string) string { return `{"password":"StrongPass1","code":123456}` },
			wantStatus: http.StatusUnauthorized, wantKey: "totp invalid code",
		},
		{
			name: "a partial xml body", contentType: "application/xml",
			body: func(code string) string {
				return `<e><Password>StrongPass1</Password><Code>` + code + `</Code><broken>`
			},
			wantStatus: http.StatusBadRequest, wantKey: "invalid password",
		},
		{
			name: "a well-formed xml body", contentType: "application/xml",
			body: func(code string) string {
				return `<e><Password>StrongPass1</Password><Code>` + code + `</Code></e>`
			},
			wantStatus: http.StatusBadRequest, wantKey: "invalid password",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTOTPSettingsContext(t, "totp-put-whole-body-"+strings.ReplaceAll(tc.name, " ", "-")+"@example.com")
			setupCookie, code, _ := enrollmentFixture(t, ctx)

			resp := send2FARequest(t, ctx, twoFARequest{
				method: http.MethodPut, contentType: tc.contentType,
				body: tc.body(code), setupCookie: setupCookie, withSession: true, withCSRFHead: true,
			})
			assert2FARefusal(t, resp, tc.wantStatus, tc.wantKey, tc.name)
			if totpEnabledInDatabase(t, ctx) {
				t.Fatal("a body the binder rejected enrolled 2FA with the code it left behind")
			}
		})
	}
}

// TestTOTPSettingsRefuseABodyTypeTheAPIDoesNotDeclare pins the transports of the
// 2FA mutations to JSON and forms: a well-formed XML body with the correct
// password (and code) is refused on both, and 2FA is left as it was.
func TestTOTPSettingsRefuseABodyTypeTheAPIDoesNotDeclare(t *testing.T) {
	for _, contentType := range []string{"application/xml", "text/xml"} {
		t.Run("PUT "+contentType, func(t *testing.T) {
			ctx := newTOTPSettingsContext(t, "totp-put-undeclared-"+strings.ReplaceAll(contentType, "/", "-")+"@example.com")
			setupCookie, code, _ := enrollmentFixture(t, ctx)

			resp := send2FARequest(t, ctx, twoFARequest{
				method: http.MethodPut, contentType: contentType,
				body:        `<e><Password>StrongPass1</Password><Code>` + code + `</Code></e>`,
				setupCookie: setupCookie, withSession: true, withCSRFHead: true,
			})
			assert2FARefusal(t, resp, http.StatusBadRequest, "invalid password", contentType)
			if totpEnabledInDatabase(t, ctx) {
				t.Fatal("an undeclared body type enrolled 2FA")
			}
		})
		t.Run("DELETE "+contentType, func(t *testing.T) {
			ctx := newTOTPSettingsContext(t, "totp-delete-undeclared-"+strings.ReplaceAll(contentType, "/", "-")+"@example.com")
			enableTOTPForSettingsTest(t, &ctx)

			resp := send2FARequest(t, ctx, twoFARequest{
				method: http.MethodDelete, contentType: contentType,
				body: `<e><Password>StrongPass1</Password></e>`, withSession: true, withCSRFHead: true,
			})
			assert2FARefusal(t, resp, http.StatusBadRequest, "invalid settings input", contentType)
			if !totpEnabledInDatabase(t, ctx) {
				t.Fatal("an undeclared body type disabled 2FA")
			}
		})
	}
}

// TestDisableTOTP2FARefusesABodyItCouldNotDecodeWhole pins the refusal to the
// bind error, not to the body's declared type: an XML body whose Password
// element decodes before the syntax error leaves a usable password in the input
// struct, and a body the binder rejected is not a submission whatever its type.
func TestDisableTOTP2FARefusesABodyItCouldNotDecodeWhole(t *testing.T) {
	partialXML := func(password string) string {
		return `<disable><Password>` + password + `</Password><broken>`
	}

	t.Run("the correct password before the syntax error is refused and 2FA stays on", func(t *testing.T) {
		ctx := newTOTPSettingsContext(t, "totp-partial-xml-correct@example.com")
		enableTOTPForSettingsTest(t, &ctx)

		resp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodDelete, contentType: "application/xml",
			body: partialXML("StrongPass1"), withSession: true, withCSRFHead: true,
		})
		assert2FARefusal(t, resp, http.StatusBadRequest, "invalid settings input", "partial xml body")
		if !totpEnabledInDatabase(t, ctx) {
			t.Fatal("a partially decoded XML body carrying the correct password disabled 2FA")
		}
	})

	t.Run("a refused body draws nothing from the failed-password budget", func(t *testing.T) {
		ctx := newTOTPSettingsContext(t, "totp-partial-xml-budget@example.com")
		enableTOTPForSettingsTest(t, &ctx)

		for attempt := range services.DefaultTOTPDisableAttemptsLimit {
			resp := send2FARequest(t, ctx, twoFARequest{
				method: http.MethodDelete, contentType: "application/xml",
				body: partialXML("WrongPass9"), withSession: true, withCSRFHead: true,
			})
			assert2FARefusal(t, resp, http.StatusBadRequest, "invalid settings input", "partial xml body with a wrong password")
			if t.Failed() {
				t.Fatalf("attempt %d was not refused as invalid input", attempt)
			}
		}

		resp := send2FARequest(t, ctx, twoFARequest{
			method: http.MethodDelete, contentType: "application/json",
			body: jsonBodyFor(map[string]string{"password": "StrongPass1"}), withSession: true, withCSRFHead: true,
		})
		assert2FAOkEnvelope(t, ctx.app, resp)
		if totpEnabledInDatabase(t, ctx) {
			t.Fatal("the failed-password budget was drawn by refused bodies: the correct JSON request did not disable 2FA")
		}
	})
}
