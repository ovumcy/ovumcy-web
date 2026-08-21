package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
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

// TestLogin_ForcedResetOutranksTOTP_RoutesToResetAndIssuesNoAuthCookie pins a
// DECISION at POST /api/v1/sessions, not merely the behaviour that happens to
// exist today: an account carrying BOTH must_change_password and totp_enabled
// is routed to /reset-password, is NOT sent to the 2FA challenge, and receives
// no ovumcy_auth session cookie on the way.
//
// The ordering is deliberate, and it is the intentional recovery path for an
// owner whose second factor is unusable. TOTP secrets are encrypted under a key
// derived from the application secret, so after a SECRET_KEY rotation the
// stored ciphertext no longer opens and no code the authenticator produces can
// ever satisfy the challenge; the operator-forced reset
// (`ovumcy reset-password <email>`) is the way back in that
// docs/security/cryptography.md and docs/self-hosted.md instruct an operator to
// use. Making TOTP win here would withdraw that escape hatch from precisely the
// accounts whose second factor is already broken, leaving permanent owner
// lockout with no in-product remedy.
//
// It is not a downgrade of session security: the reset still bumps
// AuthSessionVersion in the same atomic update that writes the new password
// hash (internal/db/user_repository.go), so every session predating it dies,
// and this response hands out a reset-password cookie rather than a session.
//
// The sibling above pins the TOTP-only account, so the two together fix the
// precedence at this route from both sides. Read a failure here as "the
// decision was reversed", not as a stale assertion. The public declaration of
// this accepted risk is recorded separately from this test.
func TestLogin_ForcedResetOutranksTOTP_RoutesToResetAndIssuesNoAuthCookie(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forced-reset-totp@example.com", "StrongPass1", true)
	setupTOTPForUser(t, database, user.ID, []byte("test-secret-key"))
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
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
		t.Fatalf("Location = %q, want /reset-password (forced reset outranks the TOTP challenge)", loc)
	}
	if c := responseCookie(resp.Cookies(), authCookieName); c != nil && strings.TrimSpace(c.Value) != "" {
		t.Error("auth cookie must not be issued on the forced-reset recovery path")
	}
	if c := responseCookie(resp.Cookies(), totpPendingCookieName); c != nil && strings.TrimSpace(c.Value) != "" {
		t.Error("TOTP pending cookie must not be issued when the forced reset wins")
	}
	resetCookie := responseCookie(resp.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatalf("expected Set-Cookie %q with non-empty value", resetPasswordCookieName)
	}
}
