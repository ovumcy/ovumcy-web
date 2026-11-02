package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// TestSessionRejectionRetractsSessionOnlyHandoffsButKeepsPreSessionOnes pins the
// selective clear a refused session performs (WEB-400). The recovery-code page,
// the TOTP enrollment seed and the calendar-feed reveal are consumable only by
// the session being refused, so they leave with it; the reset-password and
// TOTP-pending cookies are consumed without a session and may belong to a
// sign-in racing the lapsed cookie, so they stay.
func TestSessionRejectionRetractsSessionOnlyHandoffsButKeepsPreSessionOnes(t *testing.T) {
	tests := []struct {
		name       string
		authCookie func(t *testing.T, database *gorm.DB) string
	}{
		{
			name: "invalid token",
			authCookie: func(t *testing.T, _ *gorm.DB) string {
				t.Helper()
				return authCookieName + "=not-a-sealed-value"
			},
		},
		{
			name: "sealed decode failure",
			authCookie: func(t *testing.T, _ *gorm.DB) string {
				t.Helper()
				return authCookieName + "=" + secureCookieVersion + ".not-an-aead-payload"
			},
		},
		{
			name: "invalid claims",
			authCookie: func(t *testing.T, _ *gorm.DB) string {
				t.Helper()
				return authCookieName + "=" + sealAuthCookieTokenForTest(t, "not-a-signed-session-token")
			},
		},
		{
			name: "expired token",
			authCookie: func(t *testing.T, database *gorm.DB) string {
				t.Helper()
				user := createOnboardingTestUser(t, database, "reject-retract-expired@example.com", "StrongPass1", true)
				token, _, err := services.NewAuthService(nil).BuildAuthSessionTokenWithSessionID([]byte(testAppSecretKey), user.ID, user.Role, user.AuthSessionVersion, time.Hour, time.Now().Add(-2*time.Hour))
				if err != nil {
					t.Fatalf("build expired auth session token: %v", err)
				}
				return authCookieName + "=" + sealAuthCookieTokenForTest(t, token)
			},
		},
		{
			name: "revoked session",
			authCookie: func(t *testing.T, database *gorm.DB) string {
				t.Helper()
				user := createOnboardingTestUser(t, database, "reject-retract-revoked@example.com", "StrongPass1", true)
				cookie := issueAuthCookieForUser(t, user)
				if err := database.Model(&user).Update("auth_session_version", user.AuthSessionVersion+1).Error; err != nil {
					t.Fatalf("bump session version: %v", err)
				}
				return cookie
			},
		},
	}

	retracted := []string{recoveryCodeCookieName, totpSetupCookieName, calendarFeedRevealCookieName}
	kept := []string{resetPasswordCookieName, totpPendingCookieName}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)

			cookies := []string{testCase.authCookie(t, database)}
			for _, name := range append(append([]string{}, retracted...), kept...) {
				cookies = append(cookies, name+"=sealed-placeholder")
			}
			request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
			request.Header.Set("Cookie", joinCookieHeader(cookies...))

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusSeeOther)

			for _, name := range append([]string{authCookieName}, retracted...) {
				cleared := responseCookie(response.Cookies(), name)
				if cleared == nil || strings.TrimSpace(cleared.Value) != "" {
					t.Fatalf("expected the rejected session to retract %s, got %#v", name, cleared)
				}
			}
			for _, name := range kept {
				if cookie := responseCookie(response.Cookies(), name); cookie != nil {
					t.Fatalf("expected the rejected session to leave %s alone, got %#v", name, cookie)
				}
			}
		})
	}
}

func sealAuthCookieTokenForTest(t *testing.T, token string) string {
	t.Helper()
	codec, err := newSecureCookieCodec([]byte(testAppSecretKey))
	if err != nil {
		t.Fatalf("init secure cookie codec: %v", err)
	}
	sealed, err := codec.seal(authCookieName, []byte(token))
	if err != nil {
		t.Fatalf("seal auth cookie token: %v", err)
	}
	return sealed
}
