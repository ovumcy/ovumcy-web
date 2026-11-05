package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
// step issues no auth cookie, retracts the pending cookie and answers the
// "totp session expired" refusal.
//
// The operator command bumps the session version AND sets must_change_password,
// so either gate alone refuses its grant. The cases separate them: the CLI case
// runs the production reset end to end (both gates fire), the flag-only case
// writes the flag and leaves the version where the grant was minted, so only the
// must_change_password guard can be what refuses. The no-reset control issues a
// session for the same code, so a refusal is the reset and not a dead path.
func TestTOTPVerifyRefusesAPendingChallengeAfterAnOperatorForcedReset(t *testing.T) {
	cases := []struct {
		name string
		// reset applies the operator's write; nil leaves the account untouched.
		reset func(t *testing.T, database *gorm.DB, userID uint)
		// wantFlag is the must_change_password the case must leave on the row.
		wantFlag bool
		// versionHolds marks the case whose write must not move the session
		// version, so the version compare cannot be what refuses the grant.
		versionHolds bool
		// versionBumps marks the production reset, which must advance the version
		// by exactly one in the same write that sets the flag.
		versionBumps bool
		// totpKept marks the case that must leave TOTP enabled, so the verify
		// step reaches the grant check rather than an earlier branch.
		totpKept bool
		// wantSession marks the control: no reset, the valid code signs in.
		wantSession bool
	}{
		{
			name:        "no reset issues a session",
			wantSession: true,
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
			wantFlag:     true,
			versionBumps: true,
			totpKept:     true,
		},
		{
			name: "flag only, version untouched",
			reset: func(t *testing.T, database *gorm.DB, userID uint) {
				t.Helper()
				if err := database.Model(&models.User{}).Where("id = ?", userID).Update("must_change_password", true).Error; err != nil {
					t.Fatalf("set must_change_password: %v", err)
				}
			},
			wantFlag:     true,
			versionHolds: true,
			totpKept:     true,
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

			if tc.reset != nil {
				tc.reset(t, database, user.ID)
			}

			var after models.User
			if err := database.First(&after, user.ID).Error; err != nil {
				t.Fatalf("reload user: %v", err)
			}
			if after.MustChangePassword != tc.wantFlag {
				t.Fatalf("fixture: must_change_password = %v, want %v", after.MustChangePassword, tc.wantFlag)
			}
			if tc.versionHolds && after.AuthSessionVersion != granted.AuthSessionVersion {
				t.Fatalf("fixture: the version moved (%d -> %d), so the version compare could refuse the grant", granted.AuthSessionVersion, after.AuthSessionVersion)
			}
			if tc.versionBumps && after.AuthSessionVersion != granted.AuthSessionVersion+1 {
				t.Fatalf("fixture: the operator reset must bump the session version once (%d -> %d)", granted.AuthSessionVersion, after.AuthSessionVersion)
			}
			if tc.totpKept && !after.TOTPEnabled {
				t.Fatal("fixture: the reset cleared TOTP, so the verify step would stop at a different branch")
			}

			code, err := totp.GenerateCode(rawSecret, time.Now())
			if err != nil {
				t.Fatalf("GenerateCode: %v", err)
			}
			submit := func(asJSON bool) *http.Response {
				t.Helper()
				csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)
				form := url.Values{"code": {code}, "csrf_token": {csrfToken}}
				request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if asJSON {
					request.Header.Set("Accept", "application/json")
				}
				request.Header.Set("Cookie", joinCookieHeader(pendingCookie, csrfCookieHeader))
				response := mustAppResponse(t, app, request)
				t.Cleanup(func() { _ = response.Body.Close() })
				return response
			}
			hasSession := func(response *http.Response) bool {
				issued := responseCookie(response.Cookies(), authCookieName)
				return issued != nil && issued.Value != ""
			}

			if tc.wantSession {
				// The form arm lands on "/" (the post-login landing page, which
				// itself sends the owner on), as the success test above pins.
				response := submit(false)
				if !hasSession(response) || response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/" {
					t.Fatalf("control: without a forced reset the valid code must sign in to \"/\", got status %d Location %q, cookie %v", response.StatusCode, response.Header.Get("Location"), hasSession(response))
				}
				return
			}

			// A refused grant is refused on every transport: a check that ran
			// only for one of them would leave the other issuing the session.
			refusal := totpSessionExpiredErrorSpec()
			jsonResponse := submit(true)
			if hasSession(jsonResponse) {
				t.Fatal("json arm: a valid code after an operator-forced reset must not issue an auth session")
			}
			if jsonResponse.StatusCode != refusal.Status {
				t.Fatalf("json arm: expected the %d %q refusal, got status %d", refusal.Status, refusal.Key, jsonResponse.StatusCode)
			}
			if got := readAPIError(t, jsonResponse.Body); got != refusal.Key {
				t.Fatalf("json arm: expected the refusal %q, got %q", refusal.Key, got)
			}
			assertTOTPCookieCleared(t, jsonResponse, totpPendingCookieName)

			formResponse := submit(false)
			if hasSession(formResponse) {
				t.Fatal("form arm: a valid code after an operator-forced reset must not issue an auth session")
			}
			if formResponse.StatusCode != http.StatusSeeOther || formResponse.Header.Get("Location") != "/auth/2fa" {
				t.Fatalf("form arm: expected 303 back to /auth/2fa, got %d %q", formResponse.StatusCode, formResponse.Header.Get("Location"))
			}
			flash := responseCookie(formResponse.Cookies(), flashCookieName)
			if flash == nil || decodeFlashCookieForTest(t, flash.Value).AuthError != refusal.Key {
				t.Fatalf("form arm: expected the %q flash on the redirect", refusal.Key)
			}
			assertTOTPCookieCleared(t, formResponse, totpPendingCookieName)
		})
	}
}
