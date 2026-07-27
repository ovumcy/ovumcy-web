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
	if err := svc.EnableTOTP(context.Background(), userID, key.Secret()); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	return key.Secret()
}

// dbUserRepoForTest adapts *gorm.DB to services.TOTPUserRepository for test setup.
type dbUserRepoForTest struct{ db *gorm.DB }

func (r *dbUserRepoForTest) UpdateTOTPFieldsAndRevokeSessions(ctx context.Context, userID uint, encryptedSecret string, enabled bool) error {
	return r.db.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"totp_secret":          encryptedSecret,
		"totp_enabled":         enabled,
		"totp_last_used_step":  0,
		"auth_session_version": gorm.Expr("auth_session_version + 1"),
	}).Error
}

func (r *dbUserRepoForTest) UpdateTOTPSecretCiphertext(ctx context.Context, userID uint, encryptedSecret string) error {
	return r.db.Model(&models.User{}).Where("id = ?", userID).Update("totp_secret", encryptedSecret).Error
}

func (r *dbUserRepoForTest) ClaimTOTPStep(ctx context.Context, userID uint, step int64) (bool, error) {
	result := r.db.Model(&models.User{}).
		Where("id = ? AND totp_last_used_step < ?", userID, step).
		Update("totp_last_used_step", step)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookies)
	req.Header.Set("Accept-Language", "en")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST /api/v1/sessions/2fa-challenge: %v", err)
	}
	return resp
}

// --- ShowTOTPChallengePage ---

func TestShowTOTPChallengePage_MissingPendingCookie_RedirectsToLogin(t *testing.T) {
	app, _ := newOnboardingTestAppWithCSRF(t)

	req := httptest.NewRequest(http.MethodGet, "/auth/2fa", nil)
	req.Header.Set("Accept-Language", "en")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /auth/2fa: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /auth/2fa: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 303 (redirect to challenge page with error)", resp.StatusCode)
	}
	// Must NOT have issued an auth cookie
	authCookie := responseCookie(resp.Cookies(), authCookieName)
	if authCookie != nil && authCookie.Value != "" {
		t.Error("expired pending cookie should not issue an auth session")
	}
}

// TestTOTPChallengeClearsAPendingCookieItCannotUse pins the clear at the two
// handler boundaries that read `ovumcy_totp_pending`, because they answer
// differently and a clear owed by only one of them would be easy to miss: the
// challenge POST maps to the session-expired spec, the challenge page redirects
// to /login.
//
// The cookie is session-scoped at path "/", so a value the server has just
// refused kept being sent on every later request until the browser closed. The
// payload's own ExpiresAt fails closed, so this is blast radius, not a bypass —
// but a refused value has no business surviving the response that refused it.
//
// Anchored on the retry case: a wrong six-digit code is a refusal ABOUT the
// code, and it must leave the pending cookie alone, otherwise the challenge the
// response redirects back to could never be answered.
func TestTOTPChallengeClearsAPendingCookieItCannotUse(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "totp-pending-clear@example.com", "StrongPass1", true)
	secretKey := []byte("test-secret-key")
	setupTOTPForUser(t, database, user.ID, secretKey)
	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)

	validPending := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)

	// Anchor: the cookie survives a refusal that is not about the cookie.
	wrongCode := doTOTPChallengeRequest(t, app, joinCookieHeader(validPending, csrfCookieHeader), "000000", csrfToken)
	defer func() { _ = wrongCode.Body.Close() }()
	if wrongCode.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected a wrong code to redirect back to the challenge, got %d", wrongCode.StatusCode)
	}
	assertTOTPCookieLeftInPlace(t, wrongCode, totpPendingCookieName)

	tamperedPending := totpPendingCookieName + "=" + flipLastBaseEncodedByte(t, strings.TrimPrefix(validPending, totpPendingCookieName+"="))
	unusable := map[string]string{
		"expired":  sealExpiredTOTPPendingCookieForTest(t, secretKey, user.ID),
		"tampered": tamperedPending,
	}
	for name, pendingCookie := range unusable {
		t.Run(name, func(t *testing.T) {
			response := doTOTPChallengeRequest(t, app, joinCookieHeader(pendingCookie, csrfCookieHeader), "123456", csrfToken)
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusSeeOther {
				t.Fatalf("expected the challenge to refuse the %s pending cookie, got status %d", name, response.StatusCode)
			}
			if issued := responseCookie(response.Cookies(), authCookieName); issued != nil && issued.Value != "" {
				t.Fatalf("a %s pending cookie must not issue an auth session", name)
			}
			assertTOTPCookieCleared(t, response, totpPendingCookieName)
		})
	}

	// The challenge PAGE reads the same cookie and answers with a redirect
	// instead of a mapped error, so it owes the clear separately.
	t.Run("challenge_page", func(t *testing.T) {
		expiredRequest := httptest.NewRequest(http.MethodGet, "/auth/2fa", nil)
		expiredRequest.Header.Set("Accept-Language", "en")
		expiredRequest.Header.Set("Cookie", sealExpiredTOTPPendingCookieForTest(t, secretKey, user.ID))
		expiredPage := mustAppResponse(t, app, expiredRequest)

		if expiredPage.StatusCode != http.StatusSeeOther {
			t.Fatalf("expected an expired pending cookie to send the challenge page back to login, got %d", expiredPage.StatusCode)
		}
		if location := expiredPage.Header.Get("Location"); location != "/login" {
			t.Fatalf("expected a redirect to /login, got %q", location)
		}
		assertTOTPCookieCleared(t, expiredPage, totpPendingCookieName)

		// Anchor: the page still renders for a valid pending cookie, and does not
		// retract it on the way — otherwise a reload would drop the challenge.
		validRequest := httptest.NewRequest(http.MethodGet, "/auth/2fa", nil)
		validRequest.Header.Set("Accept-Language", "en")
		validRequest.Header.Set("Cookie", validPending)
		validPage := mustAppResponse(t, app, validRequest)

		if validPage.StatusCode != http.StatusOK {
			t.Fatalf("expected the challenge page to render for a valid pending cookie, got %d", validPage.StatusCode)
		}
		assertTOTPCookieLeftInPlace(t, validPage, totpPendingCookieName)
	})
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
	defer func() { _ = resp.Body.Close() }()

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

// TestVerifyTOTPLogin_AcceptsBothPublishedAndFormBodies pins the endpoint's two
// transports against each other in one test: the JSON body docs/openapi.yaml
// publishes as the request shape, and the urlencoded form the challenge page
// posts. Reading the code with c.FormValue alone answered every JSON request
// with "totp invalid code" — the same answer a wrong code gets — so an API
// client written against the published contract could never complete 2FA while
// the browser flow, and every test driving it, stayed green.
func TestVerifyTOTPLogin_AcceptsBothPublishedAndFormBodies(t *testing.T) {
	for _, transport := range []struct {
		name        string
		contentType string
		wantStatus  int
		body        func(code, csrfToken string) string
	}{
		{
			// The spec documents both outcomes for this endpoint: 200 with
			// {"ok":true} for a JSON caller, 303 for the browser form.
			name:        "json body (published contract)",
			contentType: "application/json",
			wantStatus:  http.StatusOK,
			body: func(code, _ string) string {
				return `{"code":"` + code + `"}`
			},
		},
		{
			name:        "urlencoded form (challenge page)",
			contentType: "application/x-www-form-urlencoded",
			wantStatus:  http.StatusSeeOther,
			body: func(code, csrfToken string) string {
				return url.Values{"code": {code}, "csrf_token": {csrfToken}}.Encode()
			},
		},
	} {
		t.Run(transport.name, func(t *testing.T) {
			app, database := newOnboardingTestAppWithCSRF(t)
			user := createOnboardingTestUser(t, database, "totp-transport@example.com", "StrongPass1", true)
			secretKey := []byte("test-secret-key")
			rawSecret := setupTOTPForUser(t, database, user.ID, secretKey)
			pendingCookie := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)

			code, err := totp.GenerateCode(rawSecret, time.Now())
			if err != nil {
				t.Fatalf("GenerateCode: %v", err)
			}

			csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge",
				strings.NewReader(transport.body(code, csrfToken)))
			req.Header.Set("Content-Type", transport.contentType)
			req.Header.Set("X-CSRF-Token", csrfToken)
			req.Header.Set("Cookie", joinCookieHeader(pendingCookie, csrfCookieHeader))
			req.Header.Set("Accept-Language", "en")
			resp, err := app.Test(req, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("POST /api/v1/sessions/2fa-challenge: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != transport.wantStatus {
				t.Fatalf("status = %d, want %d — the valid code was not accepted over %s",
					resp.StatusCode, transport.wantStatus, transport.contentType)
			}
			if authCookie := responseCookie(resp.Cookies(), authCookieName); authCookie == nil || authCookie.Value == "" {
				t.Errorf("expected an auth cookie after a valid code over %s", transport.contentType)
			}
		})
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
	defer func() { _ = resp.Body.Close() }()

	authCookie := responseCookie(resp.Cookies(), authCookieName)
	if authCookie != nil && authCookie.Value != "" {
		t.Error("invalid code should not issue an auth cookie")
	}

	// "No auth cookie" alone is satisfied by ANY refusal, so on its own this test
	// stayed green when the request never reached TOTP verification: a corrupted CSRF
	// token and a dropped pending cookie both look identical through that assertion.
	//
	// The browser path answers a rejected code with a redirect back to the challenge
	// (the HTMX path is the one that surfaces the raw status — see the rate-limit test
	// below). The status alone rules out a CSRF refusal, which is a 403.
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 back to the challenge, got %d — the request may have been refused before TOTP verification", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); location != "/auth/2fa" {
		t.Fatalf("expected a redirect back to /auth/2fa after a rejected code, got %q", location)
	}

	// It does NOT rule out a refusal that never reached the code check: dropping the
	// pending cookie produces the same 303 to the same place (verified by injection).
	// The rejection reason travels in the flash, so following the redirect is the only
	// way to prove the code check is what refused.
	flashValue := responseCookieValue(resp.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatal("expected a flash cookie carrying the rejection reason")
	}
	follow := httptest.NewRequest(http.MethodGet, "/auth/2fa", nil)
	follow.Header.Set("Accept-Language", "en")
	follow.Header.Set("Cookie", joinCookieHeader(pendingCookie, flashCookieName+"="+flashValue))
	followResp := mustAppResponse(t, app, follow)
	defer func() { _ = followResp.Body.Close() }()
	rendered := mustReadBodyString(t, followResp.Body)
	// The page reports the error by its LOCALE key: the flash carries the error
	// spec key ("totp invalid code") and the page resolves it through
	// AuthErrorTranslationKey before rendering, so the observable hook is
	// error.totp_invalid_code. Keyed off the spec so the two cannot drift.
	invalidCodeKey := services.AuthErrorTranslationKey(totpInvalidCodeErrorSpec().Key)
	if htmlAuthErrorByKey(mustParseHTMLDocument(t, rendered), invalidCodeKey) == nil {
		t.Fatalf("expected the invalid-code error (%s) on the challenge page; a request that never reached verification carries a session-expired flash instead, with an identical status and destination", invalidCodeKey)
	}
}

// TestVerifyTOTPLogin_RateLimited_HTMXReturns429 drives more failures than the
// configured limit through /api/v1/sessions/2fa-challenge via the HTMX path (which surfaces the
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
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.Header.Set("Cookie", joinCookieHeader(pendingCookie, csrfCookieHeader))
		req.Header.Set("Accept-Language", "en")
		resp, err := app.Test(req, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("POST /api/v1/sessions/2fa-challenge: %v", err)
		}
		return resp
	}

	for attempt := range services.DefaultTOTPAttemptsLimit {
		resp := doHTMX("000000")
		status := resp.StatusCode
		_ = resp.Body.Close()
		if status == http.StatusTooManyRequests {
			t.Fatalf("attempt %d returned 429 too early (limit is %d)", attempt+1, services.DefaultTOTPAttemptsLimit)
		}
		if status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401 (invalid code)", attempt+1, status)
		}
	}

	resp := doHTMX("000000")
	defer func() { _ = resp.Body.Close() }()
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
	_ = resp1.Body.Close()

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
	defer func() { _ = resp2.Body.Close() }()

	if c := responseCookie(resp2.Cookies(), authCookieName); c != nil && c.Value != "" {
		t.Error("replayed code must not issue a new auth cookie — replay protection failed")
	}
}

// TestVerifyTOTPLogin_InvalidCodeRendersLocalizedErrorNotTheSpecKey walks the
// real browser path of a wrong 2FA code — form POST, flash cookie, redirect back
// to the challenge page — and pins that the page renders a LOCALIZED message.
//
// The challenge page used to pass the flash value (the error spec key) straight
// into ErrorKey, and the template translates whatever ErrorKey holds; an unknown
// key comes back unchanged from translateMessage, so every user in every language
// was shown the literal string "totp invalid code". Nothing failed: the four
// error.totp_* locale entries existed all along, unreferenced.
func TestVerifyTOTPLogin_InvalidCodeRendersLocalizedErrorNotTheSpecKey(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "totp-i18n@example.com", "StrongPass1", true)
	secretKey := []byte("test-secret-key")
	setupTOTPForUser(t, database, user.ID, secretKey)
	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)

	pending := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)
	challengeResponse := doTOTPChallengeRequest(t, app, joinCookieHeader(pending, csrfCookieHeader), "000000", csrfToken)
	flashCookie := responseCookie(challengeResponse.Cookies(), flashCookieName)
	status := challengeResponse.StatusCode
	_ = challengeResponse.Body.Close()

	if status != http.StatusSeeOther {
		t.Fatalf("wrong code status = %d, want 303 (flash redirect back to the challenge page)", status)
	}
	if flashCookie == nil || flashCookie.Value == "" {
		t.Fatal("expected the wrong code to set a flash cookie — the rest of this test has no premise without it")
	}

	specKey := totpInvalidCodeErrorSpec().Key
	localeKey := services.AuthErrorTranslationKey(specKey)
	if localeKey == "" {
		t.Fatalf("error spec %q has no locale mapping — the page cannot localize it", specKey)
	}

	rendered := map[string]string{}
	for _, language := range []string{"en", "ru"} {
		request := httptest.NewRequest(http.MethodGet, "/auth/2fa", nil)
		request.Header.Set("Accept-Language", language)
		request.Header.Set("Cookie", joinCookieHeader(pending, cookiePair(flashCookie)))

		response := mustAppResponse(t, app, request)
		body := mustReadBodyString(t, response.Body)
		_ = response.Body.Close()

		errorBlock := htmlAuthErrorByKey(mustParseHTMLDocument(t, body), localeKey)
		if errorBlock == nil {
			t.Fatalf("expected the challenge page (%s) to report the error under the locale key %s after a wrong code", language, localeKey)
		}
		message := normalizeHTMLText(htmlNodeText(errorBlock))
		if strings.Contains(message, specKey) {
			t.Fatalf("challenge page (%s) rendered the raw error spec key as its message: %q", language, message)
		}
		if message == "" {
			t.Fatalf("challenge page (%s) rendered an empty error message", language)
		}
		rendered[language] = message
	}

	// Two languages resolving to the same string would mean the lookup is not
	// reaching the locale files at all — the failure mode this test exists for.
	if rendered["en"] == rendered["ru"] {
		t.Fatalf("expected per-language messages, got the same string for en and ru: %q", rendered["en"])
	}
}

// --- small helpers for extracting CSRF without a full settings context ---

func extractCSRFCookieAndToken(t *testing.T, app *fiber.App) (string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("Accept-Language", "en")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /login for csrf: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	token := extractCSRFTokenFromHTML(t, string(body))
	c := responseCookie(resp.Cookies(), "ovumcy_csrf")
	var cookieHeader string
	if c != nil {
		cookieHeader = cookiePair(c)
	}
	return token, cookieHeader
}
