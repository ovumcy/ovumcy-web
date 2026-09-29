package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
)

// The 2FA disable route re-authenticates the session user against that user's
// OWN stored password hash. It never resolves an account from the session's
// email, and its only attempt budget is totp.disable: the settings.reauth
// budget that guards erasure is not drawn here.

const (
	disableTOTPPath           = "/api/v1/users/current/2fa"
	disableTOTPInvalidKey     = "invalid credentials"
	disableTOTPRateLimitedKey = "totp too many attempts"
)

func sendDisableTOTP(t *testing.T, ctx settingsSecurityTestContext, password string) *http.Response {
	t.Helper()
	return settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, disableTOTPPath, url.Values{
		"password": {password},
	}, map[string]string{"Accept-Language": "en", "Accept": "application/json"})
}

func assertDisableTOTPRefused(t *testing.T, resp *http.Response, wantStatus int, wantKey string, label string) {
	t.Helper()
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s: status = %d, want %d", label, resp.StatusCode, wantStatus)
	}
	if got := readAPIError(t, resp.Body); got != wantKey {
		t.Fatalf("%s: error key = %q, want %q", label, got, wantKey)
	}
}

func assertDisableTOTPSucceeded(t *testing.T, ctx settingsSecurityTestContext, resp *http.Response, label string) {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: status = %d, want 200", label, resp.StatusCode)
	}
	if totpEnabledInDatabase(t, ctx) {
		t.Fatalf("%s: 2FA is still enabled after a 200", label)
	}
}

// TestDisableTOTP2FAVerifiesTheSessionOwnersOwnHashOnASharedMailbox builds the
// database the email lookup cannot serve: idx_users_email_normalized is gone
// and a second owner holds the same normalized address. The session owner's
// password must disable the session owner's 2FA and nobody else's, and the
// other account's password must not.
func TestDisableTOTP2FAVerifiesTheSessionOwnersOwnHashOnASharedMailbox(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-shared-mailbox@example.com")
	if err := ctx.database.Exec("DROP INDEX IF EXISTS idx_users_email_normalized").Error; err != nil {
		t.Fatalf("drop the normalized-email index: %v", err)
	}
	otherHash, err := bcrypt.GenerateFromPassword([]byte("OtherOwner2"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash the other owner's password: %v", err)
	}
	other := models.User{
		Email:               "TOTP-Shared-Mailbox@Example.com",
		PasswordHash:        string(otherHash),
		LocalAuthEnabled:    true,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := ctx.database.Create(&other).Error; err != nil {
		t.Fatalf("create the second owner on the shared mailbox: %v", err)
	}
	totpService := getTOTPServiceForTest(ctx.database)
	if err := totpService.EnableTOTP(context.Background(), other.ID, other.AuthSessionVersion, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP for the second owner: %v", err)
	}
	enableTOTPForSettingsTest(t, &ctx)

	// Anchor: the fixture is the one an email lookup refuses, so a handler that
	// still re-authenticated by email could not reach the 200 below.
	repositories := db.NewRepositories(ctx.database)
	if matches, err := repositories.Users.FindAllByNormalizedEmail(context.Background(), ctx.user.Email); err != nil || len(matches) != 2 {
		t.Fatalf("fixture must hold two accounts on %s, got %d (err=%v)", ctx.user.Email, len(matches), err)
	}
	if _, err := services.NewAuthService(repositories.Users).AuthenticateCredentials(context.Background(), ctx.user.Email, "StrongPass1"); err == nil {
		t.Fatal("anchor: an email lookup on the shared mailbox must refuse, or this case proves nothing")
	}

	resp := sendDisableTOTP(t, ctx, "OtherOwner2")
	assertDisableTOTPRefused(t, resp, http.StatusUnauthorized, disableTOTPInvalidKey, "the other owner's password")
	if !totpEnabledInDatabase(t, ctx) {
		t.Fatal("the other owner's password disabled the session owner's 2FA")
	}

	resp = sendDisableTOTP(t, ctx, "StrongPass1")
	assertDisableTOTPSucceeded(t, ctx, resp, "the session owner's password")

	var reloadedOther models.User
	if err := ctx.database.First(&reloadedOther, other.ID).Error; err != nil {
		t.Fatalf("reload the second owner: %v", err)
	}
	if !reloadedOther.TOTPEnabled {
		t.Fatal("disabling the session owner's 2FA also disabled the other owner's")
	}
}

// TestDisableTOTP2FAWithoutALocalPasswordIsRefusedAndDrawsTheBudget pins the
// empty-hash account (signed in through SSO only): every attempt is the same
// 401 as a wrong password, and each one is booked against totp.disable, since
// each spent an equalized bcrypt compare.
func TestDisableTOTP2FAWithoutALocalPasswordIsRefusedAndDrawsTheBudget(t *testing.T) {
	ctx := newOIDCOnlySettingsSecurityTestContext(t, "totp-disable-no-hash@example.com")
	enableTOTPForSettingsTest(t, &ctx)

	for attempt := range services.DefaultTOTPDisableAttemptsLimit {
		resp := sendDisableTOTP(t, ctx, "AnyPassword1")
		assertDisableTOTPRefused(t, resp, http.StatusUnauthorized, disableTOTPInvalidKey, "no local password, attempt "+strconv.Itoa(attempt+1))
	}

	resp := sendDisableTOTP(t, ctx, "AnyPassword1")
	assertDisableTOTPRefused(t, resp, http.StatusTooManyRequests, disableTOTPRateLimitedKey, "no local password after the budget")
	if !totpEnabledInDatabase(t, ctx) {
		t.Fatal("an account without a local password disabled 2FA")
	}
}

// TestDisableTOTP2FABudgetIsTOTPDisableOnlyAndLeavesSettingsReauthUndrawn
// spends the totp.disable budget with wrong passwords, proves it refuses the
// correct one, and then proves the erasure re-auth budget was never touched:
// the clear-data password check still accepts the correct password.
func TestDisableTOTP2FABudgetIsTOTPDisableOnlyAndLeavesSettingsReauthUndrawn(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-budget-split@example.com")
	enableTOTPForSettingsTest(t, &ctx)

	for attempt := range services.DefaultTOTPDisableAttemptsLimit {
		resp := sendDisableTOTP(t, ctx, "WrongPassword1")
		assertDisableTOTPRefused(t, resp, http.StatusUnauthorized, disableTOTPInvalidKey, "wrong password, attempt "+strconv.Itoa(attempt+1))
	}

	resp := sendDisableTOTP(t, ctx, "StrongPass1")
	assertDisableTOTPRefused(t, resp, http.StatusTooManyRequests, disableTOTPRateLimitedKey, "correct password after the budget")
	if !totpEnabledInDatabase(t, ctx) {
		t.Fatal("a rate-limited disable turned 2FA off")
	}

	validate := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe/validate", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{"Accept": "application/json"})
	if validate.StatusCode != http.StatusOK {
		t.Fatalf("clear-data validate after the 2FA budget: status = %d, want 200 — the disable drew the settings.reauth budget", validate.StatusCode)
	}
}

// TestDisableTOTP2FASuccessResetsTheDisableBudget spends all but one attempt,
// disables with the correct password, re-enables, and spends all but one
// attempt again: without the reset the second round trips the limiter early.
func TestDisableTOTP2FASuccessResetsTheDisableBudget(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-budget-reset@example.com")
	enableTOTPForSettingsTest(t, &ctx)

	for round := range 2 {
		for attempt := range services.DefaultTOTPDisableAttemptsLimit - 1 {
			resp := sendDisableTOTP(t, ctx, "WrongPassword1")
			assertDisableTOTPRefused(t, resp, http.StatusUnauthorized, disableTOTPInvalidKey,
				"round "+strconv.Itoa(round+1)+", wrong password "+strconv.Itoa(attempt+1))
		}
		resp := sendDisableTOTP(t, ctx, "StrongPass1")
		assertDisableTOTPSucceeded(t, ctx, resp, "round "+strconv.Itoa(round+1)+", correct password")

		// The disable bumped auth_session_version: reload before re-enabling
		// so EnableTOTP and the next cookie carry the current version.
		ctx.refreshAuthCookie(t)
		enableTOTPForSettingsTest(t, &ctx)
	}
}

// TestDisableTOTP2FAAcceptsAPasswordWithSurroundingWhitespace pins the trim the
// route shares with sign-in and every other settings re-auth.
func TestDisableTOTP2FAAcceptsAPasswordWithSurroundingWhitespace(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-trimmed@example.com")
	enableTOTPForSettingsTest(t, &ctx)

	resp := sendDisableTOTP(t, ctx, "  StrongPass1\t")
	assertDisableTOTPSucceeded(t, ctx, resp, "password with surrounding whitespace")
}
