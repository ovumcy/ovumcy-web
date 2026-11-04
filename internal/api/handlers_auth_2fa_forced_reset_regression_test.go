package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// TestTOTPVerifyRefusesAPendingChallengeAfterAnOperatorForcedReset pins SEC-L1
// at the handler: the owner passed the first factor and holds the pending-TOTP
// cookie, an operator then forces a reset, and the correct code at the verify
// step issues no auth cookie and retracts the pending cookie.
//
// The operator command bumps the session version AND sets must_change_password,
// so either gate alone refuses its grant. The two cases separate them: "cli"
// runs the production reset end to end (both gates fire), "flag only" writes the
// flag and leaves the version where the grant was minted, so only the
// must_change_password guard can be what refuses. The "no reset" control issues
// a session for the same code, so a refusal is the reset and not a dead path.
func TestTOTPVerifyRefusesAPendingChallengeAfterAnOperatorForcedReset(t *testing.T) {
	cases := []struct {
		name      string
		reset     func(t *testing.T, database *gorm.DB, userID uint)
		wantIssue bool
	}{
		{
			name:      "no reset issues a session",
			reset:     func(*testing.T, *gorm.DB, uint) {},
			wantIssue: true,
		},
		{
			name: "operator cli reset",
			reset: func(t *testing.T, database *gorm.DB, userID uint) {
				t.Helper()
				service := services.NewAuthService(db.NewUserRepository(database))
				if err := service.ForceResetPasswordByID(context.Background(), userID, "OperatorSetPass9!", false); err != nil {
					t.Fatalf("operator forced reset: %v", err)
				}
			},
		},
		{
			name: "flag only, version untouched",
			reset: func(t *testing.T, database *gorm.DB, userID uint) {
				t.Helper()
				if err := database.Model(&models.User{}).Where("id = ?", userID).Update("must_change_password", true).Error; err != nil {
					t.Fatalf("set must_change_password: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, database := newOnboardingTestAppWithCSRF(t)
			user := createOnboardingTestUser(t, database, "totp-forced-reset@example.com", "StrongPass1", true)
			secretKey := []byte("test-secret-key")
			rawSecret := setupTOTPForUser(t, database, user.ID, secretKey)
			pendingCookie := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)

			var granted models.User
			if err := database.First(&granted, user.ID).Error; err != nil {
				t.Fatalf("load user: %v", err)
			}
			if granted.AuthSessionVersion != 2 {
				t.Fatalf("fixture: the pending cookie is sealed at version 2, the row is at %d", granted.AuthSessionVersion)
			}

			tc.reset(t, database, user.ID)

			var after models.User
			if err := database.First(&after, user.ID).Error; err != nil {
				t.Fatalf("reload user: %v", err)
			}
			if after.MustChangePassword == tc.wantIssue {
				t.Fatalf("fixture: must_change_password = %v, want %v", after.MustChangePassword, !tc.wantIssue)
			}
			if tc.name == "flag only, version untouched" && after.AuthSessionVersion != granted.AuthSessionVersion {
				t.Fatalf("fixture: the version moved (%d -> %d), so the version compare could refuse the grant", granted.AuthSessionVersion, after.AuthSessionVersion)
			}
			if tc.name == "operator cli reset" && !after.TOTPEnabled {
				t.Fatal("fixture: the reset cleared TOTP, so the verify step would stop at a different branch")
			}

			code, err := totp.GenerateCode(rawSecret, time.Now())
			if err != nil {
				t.Fatalf("GenerateCode: %v", err)
			}
			csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)
			response := doTOTPChallengeRequest(t, app, joinCookieHeader(pendingCookie, csrfCookieHeader), code, csrfToken)
			defer func() { _ = response.Body.Close() }()

			issued := responseCookie(response.Cookies(), authCookieName)
			hasSession := issued != nil && issued.Value != ""
			if tc.wantIssue {
				if !hasSession || response.StatusCode != http.StatusSeeOther {
					t.Fatalf("control: without a forced reset the valid code must issue a session, got status %d, cookie %v", response.StatusCode, hasSession)
				}
				return
			}
			if hasSession {
				t.Fatal("a valid code after an operator-forced reset must not issue an auth session")
			}
			if response.StatusCode == http.StatusSeeOther && response.Header.Get("Location") == "/" {
				t.Fatalf("the verify step answered as a completed sign-in (303 to the landing page)")
			}
			assertTOTPCookieCleared(t, response, totpPendingCookieName)
		})
	}
}
