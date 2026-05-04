package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestLogin_TOTPEnabledUser_IssuesPendingCookieAndRedirectsTo2FAChallenge
// covers the RequiresTOTP branch of the login handler: when valid credentials
// are submitted by a user with TOTP enabled, the response must redirect to
// the 2FA challenge page, set the sealed pending cookie carrying the user's
// ID and the submitted rememberMe flag, and must NOT issue an auth session
// cookie before the second factor is verified.
func TestLogin_TOTPEnabledUser_IssuesPendingCookieAndRedirectsTo2FAChallenge(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "totp-login@example.com", "StrongPass1", true)
	secretKey := []byte("test-secret-key")
	setupTOTPForUser(t, database, user.ID, secretKey)

	form := url.Values{
		"email":       {user.Email},
		"password":    {"StrongPass1"},
		"remember_me": {"1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("POST /api/auth/login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/auth/2fa" {
		t.Errorf("Location = %q, want /auth/2fa", loc)
	}

	if c := responseCookie(resp.Cookies(), authCookieName); c != nil && c.Value != "" {
		t.Error("auth cookie must not be issued before TOTP verification succeeds")
	}

	pendingCookie := responseCookie(resp.Cookies(), totpPendingCookieName)
	if pendingCookie == nil || pendingCookie.Value == "" {
		t.Fatalf("expected Set-Cookie %q with non-empty value", totpPendingCookieName)
	}

	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	decoded, err := codec.open(totpPendingCookieName, pendingCookie.Value)
	if err != nil {
		t.Fatalf("open pending cookie: %v", err)
	}
	var payload totpPendingCookiePayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("unmarshal pending payload: %v", err)
	}
	if payload.UserID != user.ID {
		t.Errorf("pending cookie user_id = %d, want %d", payload.UserID, user.ID)
	}
	if !payload.RememberMe {
		t.Error("pending cookie remember_me = false, want true (submitted with form)")
	}
}
