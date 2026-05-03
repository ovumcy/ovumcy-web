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

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// --- helpers ---

func setupTOTPForUser(t *testing.T, database *gorm.DB, userID uint, secretKey []byte) string {
	t.Helper()
	svc := services.NewTOTPService(&dbUserRepoForTest{database}, secretKey, nil)
	key, err := svc.GenerateSetupKey("Ovumcy", "test@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	if err := svc.EnableTOTP(userID, key.Secret()); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	return key.Secret()
}

// dbUserRepoForTest adapts *gorm.DB to services.TOTPUserRepository for test setup.
type dbUserRepoForTest struct{ db *gorm.DB }

func (r *dbUserRepoForTest) UpdateTOTPFields(userID uint, encryptedSecret string, enabled bool) error {
	return r.db.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"totp_secret":  encryptedSecret,
		"totp_enabled": enabled,
	}).Error
}

func sealTOTPPendingCookieForTest(t *testing.T, secretKey []byte, userID uint, rememberMe bool) string {
	t.Helper()
	payload := totpPendingCookiePayload{
		UserID:     userID,
		RememberMe: rememberMe,
		ExpiresAt:  time.Now().Add(5 * time.Minute),
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal totp pending payload: %v", err)
	}
	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	sealed, err := codec.seal(totpPendingCookieName, serialized)
	if err != nil {
		t.Fatalf("seal totp pending: %v", err)
	}
	return totpPendingCookieName + "=" + sealed
}

func sealExpiredTOTPPendingCookieForTest(t *testing.T, secretKey []byte, userID uint) string {
	t.Helper()
	payload := totpPendingCookiePayload{
		UserID:    userID,
		ExpiresAt: time.Now().Add(-1 * time.Minute), // already expired
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal expired totp pending payload: %v", err)
	}
	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	sealed, err := codec.seal(totpPendingCookieName, serialized)
	if err != nil {
		t.Fatalf("seal expired totp pending: %v", err)
	}
	return totpPendingCookieName + "=" + sealed
}

func doTOTPChallengeRequest(t *testing.T, app *fiber.App, cookies string, code string, csrfToken string) *http.Response {
	t.Helper()
	form := url.Values{"code": {code}, "csrf_token": {csrfToken}}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/2fa", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookies)
	req.Header.Set("Accept-Language", "en")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("POST /api/auth/2fa: %v", err)
	}
	return resp
}

// --- ShowTOTPChallengePage ---

func TestShowTOTPChallengePage_MissingPendingCookie_RedirectsToLogin(t *testing.T) {
	app, _ := newOnboardingTestAppWithCSRF(t)

	req := httptest.NewRequest(http.MethodGet, "/auth/2fa", nil)
	req.Header.Set("Accept-Language", "en")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET /auth/2fa: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func TestShowTOTPChallengePage_ValidPendingCookie_Renders200(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "totp-page@example.com", "StrongPass1", true)
	secretKey := []byte("test-secret-key")
	pendingCookie := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)

	req := httptest.NewRequest(http.MethodGet, "/auth/2fa", nil)
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", pendingCookie)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET /auth/2fa: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "two-factor") && !strings.Contains(strings.ToLower(string(body)), "authentication") {
		t.Error("challenge page body does not mention authentication")
	}
}

// --- VerifyTOTPLogin ---

func TestVerifyTOTPLogin_MissingPendingCookie_ReturnsError(t *testing.T) {
	app, _ := newOnboardingTestAppWithCSRF(t)
	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)

	resp := doTOTPChallengeRequest(t, app, csrfCookieHeader, "123456", csrfToken)
	defer resp.Body.Close()

	// HTML form path: respondAuthError redirects with 303 to /auth/2fa.
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 303 (redirect to challenge page with error)", resp.StatusCode)
	}
	if c := responseCookie(resp.Cookies(), authCookieName); c != nil && c.Value != "" {
		t.Error("missing pending cookie must not issue an auth cookie")
	}
}

func TestVerifyTOTPLogin_ExpiredPendingCookie_ReturnsError(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "totp-expired@example.com", "StrongPass1", true)
	secretKey := []byte("test-secret-key")
	expiredCookie := sealExpiredTOTPPendingCookieForTest(t, secretKey, user.ID)
	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)

	resp := doTOTPChallengeRequest(t, app, joinCookieHeader(expiredCookie, csrfCookieHeader), "123456", csrfToken)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 303 (redirect to challenge page with error)", resp.StatusCode)
	}
	// Must NOT have issued an auth cookie
	authCookie := responseCookie(resp.Cookies(), authCookieName)
	if authCookie != nil && authCookie.Value != "" {
		t.Error("expired pending cookie should not issue an auth session")
	}
}

func TestVerifyTOTPLogin_ValidCode_IssuesSessionAndRedirects(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "totp-valid@example.com", "StrongPass1", true)
	secretKey := []byte("test-secret-key")
	rawSecret := setupTOTPForUser(t, database, user.ID, secretKey)
	pendingCookie := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)

	code, err := totp.GenerateCode(rawSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)
	cookies := joinCookieHeader(pendingCookie, csrfCookieHeader)
	resp := doTOTPChallengeRequest(t, app, cookies, code, csrfToken)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", resp.StatusCode)
	}
	authCookie := responseCookie(resp.Cookies(), authCookieName)
	if authCookie == nil || authCookie.Value == "" {
		t.Error("expected auth cookie after successful TOTP verification")
	}
	pendingAfter := responseCookie(resp.Cookies(), totpPendingCookieName)
	if pendingAfter != nil && pendingAfter.Value != "" && pendingAfter.Expires.After(time.Now()) {
		t.Error("expected pending cookie to be cleared after successful TOTP")
	}
}

func TestVerifyTOTPLogin_InvalidCode_DoesNotIssueSession(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "totp-invalid@example.com", "StrongPass1", true)
	secretKey := []byte("test-secret-key")
	setupTOTPForUser(t, database, user.ID, secretKey)
	pendingCookie := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)

	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)
	cookies := joinCookieHeader(pendingCookie, csrfCookieHeader)
	// "000000" is almost certainly invalid
	resp := doTOTPChallengeRequest(t, app, cookies, "000000", csrfToken)
	defer resp.Body.Close()

	authCookie := responseCookie(resp.Cookies(), authCookieName)
	if authCookie != nil && authCookie.Value != "" {
		t.Error("invalid code should not issue an auth cookie")
	}
}

// TestVerifyTOTPLogin_RateLimited_HTMXReturns429 drives more failures than the
// configured limit through /api/auth/2fa via the HTMX path (which surfaces the
// real status code) and asserts the 6th attempt is rejected with 429 by the
// rate limiter. Guards against accidental removal of the CheckRateLimit call
// in the handler or wiring breakage between handler and service.
func TestVerifyTOTPLogin_RateLimited_HTMXReturns429(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "totp-ratelimit@example.com", "StrongPass1", true)
	secretKey := []byte("test-secret-key")
	setupTOTPForUser(t, database, user.ID, secretKey)
	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)

	doHTMX := func(code string) *http.Response {
		t.Helper()
		pendingCookie := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)
		form := url.Values{"code": {code}, "csrf_token": {csrfToken}}
		req := httptest.NewRequest(http.MethodPost, "/api/auth/2fa", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.Header.Set("Cookie", joinCookieHeader(pendingCookie, csrfCookieHeader))
		req.Header.Set("Accept-Language", "en")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("POST /api/auth/2fa: %v", err)
		}
		return resp
	}

	for attempt := 0; attempt < services.DefaultTOTPAttemptsLimit; attempt++ {
		resp := doHTMX("000000")
		status := resp.StatusCode
		resp.Body.Close()
		if status == http.StatusTooManyRequests {
			t.Fatalf("attempt %d returned 429 too early (limit is %d)", attempt+1, services.DefaultTOTPAttemptsLimit)
		}
		if status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401 (invalid code)", attempt+1, status)
		}
	}

	resp := doHTMX("000000")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status after %d failed attempts = %d, want 429", services.DefaultTOTPAttemptsLimit, resp.StatusCode)
	}
	if c := responseCookie(resp.Cookies(), authCookieName); c != nil && c.Value != "" {
		t.Error("rate-limited request must not issue an auth cookie")
	}
}

// TestVerifyTOTPLogin_ReplayCode_Rejected proves the handler rejects a TOTP
// code that has already been consumed for the same user. Guards against
// removal of the replay check in ValidateCode or its wiring in the handler.
func TestVerifyTOTPLogin_ReplayCode_Rejected(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "totp-replay@example.com", "StrongPass1", true)
	secretKey := []byte("test-secret-key")
	rawSecret := setupTOTPForUser(t, database, user.ID, secretKey)

	code, err := totp.GenerateCode(rawSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)

	pending1 := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)
	resp1 := doTOTPChallengeRequest(t, app, joinCookieHeader(pending1, csrfCookieHeader), code, csrfToken)
	status1 := resp1.StatusCode
	cookies1 := resp1.Cookies()
	resp1.Body.Close()

	if status1 != http.StatusSeeOther {
		t.Fatalf("first submission status = %d, want 303", status1)
	}
	if c := responseCookie(cookies1, authCookieName); c == nil || c.Value == "" {
		t.Fatal("first submission did not issue an auth cookie — replay test premise is broken")
	}

	// Replay the same code with a fresh pending cookie. Replay protection must
	// reject it; no new auth cookie may be issued.
	pending2 := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)
	resp2 := doTOTPChallengeRequest(t, app, joinCookieHeader(pending2, csrfCookieHeader), code, csrfToken)
	defer resp2.Body.Close()

	if c := responseCookie(resp2.Cookies(), authCookieName); c != nil && c.Value != "" {
		t.Error("replayed code must not issue a new auth cookie — replay protection failed")
	}
}

// --- small helpers for extracting CSRF without a full settings context ---

func extractCSRFCookieAndToken(t *testing.T, app *fiber.App) (string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("Accept-Language", "en")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET /login for csrf: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	token := extractCSRFTokenFromHTML(t, string(body))
	c := responseCookie(resp.Cookies(), "ovumcy_csrf")
	var cookieHeader string
	if c != nil {
		cookieHeader = cookiePair(c)
	}
	return token, cookieHeader
}
