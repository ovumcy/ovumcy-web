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

	"github.com/gofiber/fiber/v3"
	"github.com/pquerna/otp/totp"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Both 2FA mutations used to content-negotiate on HX-Request alone: an HTMX
// caller got the status markup and EVERY other caller — an API client asking
// for JSON included — got 303 to /settings, a response with no body at all for
// a client that cannot follow a browser redirect. The two tests below are the
// pair the sibling settings mutations already have (ClearAllData,
// DeleteAccount): a JSON caller gets the ok-envelope docs/openapi.yaml declares
// on the 200, and no Location header.
func assert2FAOkEnvelope(t *testing.T, app *fiber.App, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// The redirect is the whole regression: a 303 also carries no body, so
	// asserting the body alone would pass on a response the client cannot use.
	if location := resp.Header.Get("Location"); location != "" {
		t.Errorf("JSON caller was redirected to %q; want the JSON body instead", location)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("body %q is not JSON: %v", string(body), err)
	}
	if decoded["ok"] != true {
		t.Errorf("body = %q, want the ok envelope", string(body))
	}
	// OkResponse declares additionalProperties: false, so a second key would be
	// a body the published schema rejects.
	if len(decoded) != 1 {
		t.Errorf("body = %q, want exactly the declared `ok` property", string(body))
	}
	// Toggling 2FA bumps auth_session_version, so the answer is only usable if
	// it also re-issues this device's cookie — asserted here and not only on the
	// HTMX arm, because the JSON arm returns before the redirect and a refactor
	// moving it above refreshCurrentSession would sign the caller out on its
	// next request with every status assertion still green.
	refreshed := responseCookie(resp.Cookies(), authCookieName)
	if refreshed == nil || strings.TrimSpace(refreshed.Value) == "" {
		t.Fatal("the JSON answer must re-issue ovumcy_auth")
	}
	if !authCookieAuthenticates(t, app, refreshed.Value) {
		t.Error("the re-issued cookie must authenticate; a stale session version would reject it")
	}
}

func TestVerifyTOTP2FAEnrollmentAnswersAJSONCallerWithTheDeclaredBody(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-json@example.com")

	key, err := services.NewTOTPService(&dbUserRepoForTest{ctx.database}, []byte("test-secret-key"), nil).GenerateSetupKey("Ovumcy", ctx.user.Email)
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	setupCookie := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), ctx.user.ID, key.Secret())

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	form := cloneFormValues(url.Values{"code": {code}, "password": {"StrongPass1"}})
	form.Set("csrf_token", ctx.csrfToken)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/current/2fa", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))
	resp, err := ctx.app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("PUT /api/v1/users/current/2fa: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assert2FAOkEnvelope(t, ctx.app, resp)
}

func TestDisableTOTP2FAAnswersAJSONCallerWithTheDeclaredBody(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-json@example.com")
	if err := getTOTPServiceForTest(ctx.database).EnableTOTP(context.Background(), ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	// EnableTOTP bumped auth_session_version, so the pre-enable cookie would
	// die at AuthRequired and the request would never reach the handler.
	ctx.refreshAuthCookie(t)

	form := url.Values{"password": {"StrongPass1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/2fa", form,
		map[string]string{"Accept-Language": "en", "Accept": "application/json"})
	defer func() { _ = resp.Body.Close() }()

	assert2FAOkEnvelope(t, ctx.app, resp)
}
