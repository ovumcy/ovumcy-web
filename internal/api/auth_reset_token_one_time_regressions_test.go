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
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestResetPasswordTokenCannotBeReusedAfterSuccessfulReset(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-one-time@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookieValue := requestResetCookieByRecoveryCode(t, app, recoveryCode)

	firstResetForm := url.Values{
		"password":         {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}
	firstResetRequest := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader(firstResetForm.Encode()))
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
	secondResetRequest := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader(secondResetForm.Encode()))
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

	request := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader(url.Values{
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
			request := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader(url.Values{
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

func requestResetCookieByRecoveryCode(t *testing.T, app *fiber.App, recoveryCode string) string {
	t.Helper()

	form := url.Values{"recovery_code": {recoveryCode}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader(form.Encode()))
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
