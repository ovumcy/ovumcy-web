package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestResetPasswordTokenCannotBeReusedAfterSuccessfulReset(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-one-time@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookieValue := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode)

	firstResetForm := url.Values{
		"password":         {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	firstResetRequest := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(firstResetForm.Encode()))
	firstResetRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	firstResetRequest.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)

	firstResetResponse, err := app.Test(firstResetRequest, -1)
	if err != nil {
		t.Fatalf("first reset-password request failed: %v", err)
	}
	defer firstResetResponse.Body.Close()

	if firstResetResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected first reset status 303, got %d", firstResetResponse.StatusCode)
	}
	if location := firstResetResponse.Header.Get("Location"); location != "/recovery-code" {
		t.Fatalf("expected first reset redirect /recovery-code, got %q", location)
	}

	secondResetForm := url.Values{
		"password":         {"AnotherStrong3"},
		"confirm_password": {"AnotherStrong3"},
	}
	secondResetRequest := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(secondResetForm.Encode()))
	secondResetRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	secondResetRequest.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)

	secondResetResponse, err := app.Test(secondResetRequest, -1)
	if err != nil {
		t.Fatalf("second reset-password request failed: %v", err)
	}
	defer secondResetResponse.Body.Close()

	if secondResetResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected second reset status 303, got %d", secondResetResponse.StatusCode)
	}
	if location := secondResetResponse.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected second reset redirect /reset-password, got %q", location)
	}
}

func TestResetPasswordRejectsExpiredResetToken(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-expired-token@example.com", "StrongPass1", true)

	expiredToken := mustSignResetTokenForTest(t, user.ID, user.PasswordHash, time.Now().Add(-5*time.Minute), time.Now().Add(-30*time.Minute))
	resetCookieValue := mustSealResetCookieValueForTest(t, []byte("test-secret-key"), expiredToken, false)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(url.Values{
		"password":         {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("reset-password request with expired token failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected redirect /reset-password, got %q", location)
	}
}

func TestResetPasswordRejectsInvalidOrTamperedResetToken(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-invalid-token@example.com", "StrongPass1", true)

	validToken := mustSignResetTokenForTest(t, user.ID, user.PasswordHash, time.Now().Add(10*time.Minute), time.Now())
	tamperedToken := mustTamperResetTokenSignatureForTest(t, validToken)

	testCases := []struct {
		name       string
		tokenValue string
	}{
		{name: "invalid-format", tokenValue: "not-a-jwt-token"},
		{name: "tampered-signature", tokenValue: tamperedToken},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resetCookieValue := mustSealResetCookieValueForTest(t, []byte("test-secret-key"), tc.tokenValue, false)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(url.Values{
				"password":         {"EvenStronger2"},
				"confirm_password": {"EvenStronger2"},
			}.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)

			response, err := app.Test(request, -1)
			if err != nil {
				t.Fatalf("reset-password request failed: %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != http.StatusSeeOther {
				t.Fatalf("expected status 303, got %d", response.StatusCode)
			}
			if location := response.Header.Get("Location"); location != "/reset-password" {
				t.Fatalf("expected redirect /reset-password, got %q", location)
			}
		})
	}
}

// TestOriginalRecoveryCodeRejectedAfterCompletedReset closes the HTTP-level
// gap around recovery-code rotation: completing a password reset issues a
// NEW recovery code (rotation is unit-pinned in
// internal/services/auth_service_recovery_test.go), so the ORIGINAL code
// must stop working for any later recovery attempt. Without this regression
// a transport-layer refactor could keep accepting the stale code even
// though the service rotated it.
func TestOriginalRecoveryCodeRejectedAfterCompletedReset(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "recovery-rotation-http@example.com", "StrongPass1", true)

	originalRecoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookieValue := requestResetCookieByRecoveryCode(t, app, user.Email, originalRecoveryCode)

	resetForm := url.Values{
		"password":         {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	resetRequest := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(resetForm.Encode()))
	resetRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resetRequest.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)

	resetResponse, err := app.Test(resetRequest, -1)
	if err != nil {
		t.Fatalf("reset-password request failed: %v", err)
	}
	defer resetResponse.Body.Close()
	if resetResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected reset status 303, got %d", resetResponse.StatusCode)
	}
	if location := resetResponse.Header.Get("Location"); location != "/recovery-code" {
		t.Fatalf("expected completed reset to redirect /recovery-code, got %q", location)
	}

	// A fresh recovery attempt with the ORIGINAL code must be rejected:
	// no redirect into the reset flow and no usable reset cookie.
	retryForm := url.Values{
		"email":         {user.Email},
		"recovery_code": {originalRecoveryCode},
	}
	retryRequest := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets", strings.NewReader(retryForm.Encode()))
	retryRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	retryResponse, err := app.Test(retryRequest, -1)
	if err != nil {
		t.Fatalf("retry forgot-password request failed: %v", err)
	}
	defer retryResponse.Body.Close()

	if location := retryResponse.Header.Get("Location"); location == "/reset-password" {
		t.Fatalf("stale recovery code must not enter the reset flow, got redirect to %q", location)
	}
	if cookie := responseCookie(retryResponse.Cookies(), resetPasswordCookieName); cookie != nil && strings.TrimSpace(cookie.Value) != "" {
		t.Fatalf("stale recovery code must not mint a reset cookie, got %q", cookie.Value)
	}
}

func requestResetCookieByRecoveryCode(t *testing.T, app *fiber.App, email string, recoveryCode string) string {
	t.Helper()

	form := url.Values{
		"email":         {email},
		"recovery_code": {recoveryCode},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("forgot-password request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected forgot-password status 303, got %d", response.StatusCode)
	}
	resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatalf("expected reset-password cookie in forgot-password response")
	}
	return resetCookie.Value
}

func mustSignResetTokenForTest(t *testing.T, userID uint, passwordHash string, expiresAt time.Time, issuedAt time.Time) string {
	t.Helper()

	claims := services.PasswordResetClaims{
		UserID:        userID,
		Purpose:       "password_reset",
		PasswordState: services.PasswordStateFingerprint(passwordHash),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(uint64(userID), 10),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("sign reset token for test: %v", err)
	}
	return signed
}

func mustSealResetCookieValueForTest(t *testing.T, secretKey []byte, token string, forced bool) string {
	t.Helper()

	payload := resetPasswordCookiePayload{
		Token:  token,
		Forced: forced,
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal reset cookie payload: %v", err)
	}

	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}
	encoded, err := codec.seal(resetPasswordCookieName, serialized)
	if err != nil {
		t.Fatalf("seal reset cookie payload: %v", err)
	}
	return encoded
}

func mustTamperResetTokenSignatureForTest(t *testing.T, token string) string {
	t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected signed jwt format, got %q", token)
	}

	signature := parts[2]
	if signature == "" {
		t.Fatalf("expected non-empty jwt signature")
	}

	mutatedFirst := "A"
	if strings.HasPrefix(signature, "A") {
		mutatedFirst = "B"
	}
	parts[2] = mutatedFirst + signature[1:]
	return strings.Join(parts, ".")
}

// ShowResetPasswordPage handler-level coverage. The page composes three
// signals that the user-facing UI keys off: invalid-token notice, forced
// reset notice, and flash auth-error key. Each is exposed through stable
// data-* hooks; these tests pin the rendering contract without depending on
// localized copy.

func TestShowResetPasswordPageWithoutCookieRendersForm(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/reset-password", nil))
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, body,
		bodyStringMatch{fragment: `id="reset-password-form"`, message: "expected reset form rendered when no cookie present"},
	)
	assertBodyNotContainsAll(t, body,
		bodyStringMatch{fragment: `data-reset-notice="invalid-token"`, message: "did not expect invalid-token notice without any cookie"},
		bodyStringMatch{fragment: `data-reset-notice="forced"`, message: "did not expect forced-reset notice without any cookie"},
		bodyStringMatch{fragment: `data-auth-server-error`, message: "did not expect server error block without a flash auth error"},
	)
}

func TestShowResetPasswordPageWithInvalidCookieShowsInvalidTokenNoticeAndClearsCookie(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	cookieValue := mustSealResetCookieValueForTest(t, []byte(testHandlerSecretKey), "obviously-not-a-jwt", false)
	request := httptest.NewRequest(http.MethodGet, "/reset-password", nil)
	request.Header.Set("Cookie", resetPasswordCookieName+"="+cookieValue)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, body,
		bodyStringMatch{fragment: `data-reset-notice="invalid-token"`, message: "expected invalid-token notice for malformed reset token"},
	)
	assertBodyNotContainsAll(t, body,
		bodyStringMatch{fragment: `id="reset-password-form"`, message: "expected form to be hidden when token is invalid"},
	)

	clearedCookie := responseCookie(response.Cookies(), resetPasswordCookieName)
	if clearedCookie == nil {
		t.Fatal("expected reset-password cookie to be cleared on invalid token")
	}
	if clearedCookie.Value != "" {
		t.Fatalf("expected cleared reset cookie value, got %q", clearedCookie.Value)
	}
}

// TestShowResetPasswordPageWithValidNonForcedTokenShowsFormOnly locks the
// recovery-initiated branch (non-forced reset). The page must render the
// form but neither the forced notice nor the invalid-token notice. Without
// this regression a refactor that always set ForcedReset=true (or never
// set it) would silently shift the recovery UX without failing any
// existing test.
func TestShowResetPasswordPageWithValidNonForcedTokenShowsFormOnly(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-page-recovery@example.com", "StrongPass1", true)

	token, err := services.BuildPasswordResetToken([]byte(testHandlerSecretKey), user.ID, user.PasswordHash, 30*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("BuildPasswordResetToken: %v", err)
	}
	cookieValue := mustSealResetCookieValueForTest(t, []byte(testHandlerSecretKey), token, false)

	request := httptest.NewRequest(http.MethodGet, "/reset-password", nil)
	request.Header.Set("Cookie", resetPasswordCookieName+"="+cookieValue)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, body,
		bodyStringMatch{fragment: `id="reset-password-form"`, message: "expected form rendered for valid non-forced token"},
	)
	assertBodyNotContainsAll(t, body,
		bodyStringMatch{fragment: `data-reset-notice="forced"`, message: "did not expect forced-reset notice for recovery (non-forced) token"},
		bodyStringMatch{fragment: `data-reset-notice="invalid-token"`, message: "did not expect invalid-token notice for valid recovery token"},
	)
}

func TestShowResetPasswordPageWithValidForcedTokenShowsForcedNoticeAndForm(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-page-forced@example.com", "StrongPass1", true)

	token, err := services.BuildPasswordResetToken([]byte(testHandlerSecretKey), user.ID, user.PasswordHash, 30*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("BuildPasswordResetToken: %v", err)
	}
	cookieValue := mustSealResetCookieValueForTest(t, []byte(testHandlerSecretKey), token, true)

	request := httptest.NewRequest(http.MethodGet, "/reset-password", nil)
	request.Header.Set("Cookie", resetPasswordCookieName+"="+cookieValue)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, body,
		bodyStringMatch{fragment: `data-reset-notice="forced"`, message: "expected forced-reset notice for forced token"},
		bodyStringMatch{fragment: `id="reset-password-form"`, message: "expected form rendered for valid token"},
	)
	assertBodyNotContainsAll(t, body,
		bodyStringMatch{fragment: `data-reset-notice="invalid-token"`, message: "did not expect invalid-token notice for valid token"},
	)
}

// TestShowResetPasswordPageSurfacesFlashAuthErrorThroughDataKey locks the
// flash-handoff contract: a redirect from the redeem endpoint with an auth
// error must surface that error key on the GET page via the
// data-auth-server-error / data-error-key hooks the front-end keys off.
func TestShowResetPasswordPageSurfacesFlashAuthErrorThroughDataKey(t *testing.T) {
	handler := &Handler{
		secretKey:    []byte(testHandlerSecretKey),
		cookieSecure: true,
	}
	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	flashBytes, err := json.Marshal(FlashPayload{AuthError: "weak password"})
	if err != nil {
		t.Fatalf("marshal flash: %v", err)
	}
	flashCookieValue, err := codec.seal(flashCookieName, flashBytes)
	if err != nil {
		t.Fatalf("seal flash cookie: %v", err)
	}

	app, _ := newOnboardingTestApp(t)
	request := httptest.NewRequest(http.MethodGet, "/reset-password", nil)
	request.Header.Set("Cookie", flashCookieName+"="+flashCookieValue)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, body,
		bodyStringMatch{fragment: `data-auth-server-error`, message: "expected auth server error wrapper for flash AuthError"},
		bodyStringMatch{fragment: `data-error-key="auth.error.weak_password"`, message: "expected localized error key surfaced via data-error-key"},
	)

	clearedFlash := responseCookie(response.Cookies(), flashCookieName)
	if clearedFlash == nil {
		t.Fatal("expected flash cookie to be cleared after pop")
	}
	if clearedFlash.Value != "" {
		t.Fatalf("expected cleared flash cookie value, got %q", clearedFlash.Value)
	}
}
