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

// TestForcedResetTOTPAccountRedeemsToALiveSessionWithoutASecondFactor drives
// BOTH accepted-risk halves on one account end to end: TOTP enrolled AND
// must_change_password set. The sibling above already pins that login never
// reaches the 2FA challenge on such an account, and
// TestForcedResetRedeemSurvivesLocalPublicAuthBeingOff already pins that the
// redeem still succeeds once local public auth is off — but nothing before
// this test drives login THEN redeem on the same TOTP-enabled account and
// checks that what comes out the other end is a session that actually works,
// which is the complete shape docs/security/known-disclosures.md documents
// under "Password reset issues a session without a second-factor challenge".
//
// This documents the accepted risk; it does not refute it. Read a failure
// here as "the decision was reversed", not as a stale assertion: either TOTP
// became reachable somewhere on the path (a 2FA-pending cookie at login, a
// totp_code requirement on redeem, a refused session afterward), or the
// redeemed session never becomes live.
func TestForcedResetTOTPAccountRedeemsToALiveSessionWithoutASecondFactor(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forced-reset-totp-e2e@example.com", "StrongPass1", true)
	setupTOTPForUser(t, database, user.ID, []byte("test-secret-key"))
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}

	// Step 1: log in with email + password only — no totp_code anywhere in the
	// submission. The forced reset outranks TOTP at this route (pinned by the
	// sibling test above), so this must land on /reset-password, never on the
	// 2FA challenge.
	loginForm := url.Values{
		"email":    {user.Email},
		"password": {"StrongPass1"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(loginForm.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResp, err := app.Test(loginReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST /api/v1/sessions: %v", err)
	}
	defer func() { _ = loginResp.Body.Close() }()

	if loginResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", loginResp.StatusCode)
	}
	if loc := loginResp.Header.Get("Location"); loc != "/reset-password" {
		t.Fatalf("login Location = %q, want /reset-password", loc)
	}
	if c := responseCookie(loginResp.Cookies(), authCookieName); c != nil && strings.TrimSpace(c.Value) != "" {
		t.Fatal("login must not mint an auth session before redeem")
	}
	if c := responseCookie(loginResp.Cookies(), totpPendingCookieName); c != nil && strings.TrimSpace(c.Value) != "" {
		t.Fatal("TOTP must never be demanded on the forced-reset branch: no pending-2FA cookie may be issued at login")
	}
	resetCookie := responseCookieValue(loginResp.Cookies(), resetPasswordCookieName)
	if resetCookie == "" {
		t.Fatal("expected a reset-password cookie from the forced-reset login")
	}

	// Step 2: redeem the reset token. The submission carries only a new
	// password — the redeem form has no totp_code field, and none is sent.
	redeemForm := url.Values{
		"password":         {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	redeemReq := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(redeemForm.Encode()))
	redeemReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	redeemReq.Header.Set("Accept", "application/json")
	redeemReq.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookie)
	redeemResp, err := app.Test(redeemReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST /api/v1/password-resets/redeem: %v", err)
	}
	defer func() { _ = redeemResp.Body.Close() }()

	if redeemResp.StatusCode != http.StatusOK {
		t.Fatalf("redeem status = %d, want 200", redeemResp.StatusCode)
	}
	authCookie := responseCookieValue(redeemResp.Cookies(), authCookieName)
	if authCookie == "" {
		t.Fatal("expected the redeem to mint an auth session cookie")
	}
	if c := responseCookie(redeemResp.Cookies(), totpPendingCookieName); c != nil && strings.TrimSpace(c.Value) != "" {
		t.Fatal("TOTP must never be demanded on redeem either: no pending-2FA cookie may be issued")
	}

	// Step 3: the cookie the redeem just handed out must be a genuinely LIVE
	// session, not merely a Set-Cookie header. ResolveAuthSession
	// (internal/services/auth_service.go) refuses any token whose account
	// still carries must_change_password, so a 200 here proves the flag was
	// cleared by the redeem write and the second factor was never consulted
	// anywhere on the way to a usable session.
	currentReq := httptest.NewRequest(http.MethodGet, "/api/v1/users/current", nil)
	currentReq.Header.Set("Cookie", authCookieName+"="+authCookie)
	currentResp, err := app.Test(currentReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /api/v1/users/current: %v", err)
	}
	defer func() { _ = currentResp.Body.Close() }()
	if currentResp.StatusCode != http.StatusOK {
		t.Fatalf("current-user status = %d, want 200 (the redeemed session must be live)", currentResp.StatusCode)
	}

	// The account's TOTP enrollment survives the whole path untouched: the
	// accepted risk is that the challenge is skipped, not that enrollment is
	// silently dropped along the way.
	var stored models.User
	if err := database.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !stored.TOTPEnabled {
		t.Fatal("expected TOTP to remain enabled on the account after the forced-reset redeem")
	}
	if stored.MustChangePassword {
		t.Fatal("expected must_change_password to be cleared by the redeem")
	}
}
