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
	svc := services.NewTOTPService(&dbUserRepoForTest{database}, secretKey)
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
	csrfToken := extractGlobalCSRFToken(t, app)

	resp := doTOTPChallengeRequest(t, app, getCsrfCookieHeader(t, app), "123456", csrfToken)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusSeeOther {
		t.Errorf("expected error status, got %d", resp.StatusCode)
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

// --- small helpers for extracting CSRF without a full settings context ---

func extractGlobalCSRFToken(t *testing.T, app *fiber.App) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("Accept-Language", "en")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET /login for csrf: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return extractCSRFTokenFromHTML(t, string(body))
}

func getCsrfCookieHeader(t *testing.T, app *fiber.App) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("Accept-Language", "en")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET /login for csrf cookie: %v", err)
	}
	defer resp.Body.Close()
	c := responseCookie(resp.Cookies(), "ovumcy_csrf")
	if c == nil {
		return ""
	}
	return cookiePair(c)
}

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
