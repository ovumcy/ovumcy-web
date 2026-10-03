package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// The 2FA disable route re-authenticates the session user against that user's
// OWN stored password hash. It never resolves an account from the session's
// email, and it draws the account's one password re-auth budget — the same
// settings.reauth bucket the erasure and password-change checks draw.

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
	if err := totpService.EnableTOTP(context.Background(), other.ID, other.AuthSessionVersion, "JBSWY3DPEHPK3PXP", verifiedEnrollmentStepForTest(t, "JBSWY3DPEHPK3PXP")); err != nil {
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
// 401 as a wrong password, and each one is booked against the re-auth budget, since
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

// TestDisableTOTP2FASpentBudgetRefusesTheSettingsReauth spends the 2FA
// disable's budget with wrong passwords, proves it refuses the correct one, and
// then proves the erasure re-auth is refused too: both draw the account's one
// password re-auth budget, so a session gets no more guesses at the password
// than the sign-in form allows.
func TestDisableTOTP2FASpentBudgetRefusesTheSettingsReauth(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-budget-shared@example.com")
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
	if validate.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("clear-data validate after the 2FA budget: status = %d, want 429 — the disable drew a budget of its own", validate.StatusCode)
	}
}

// TestSettingsReauthSpentBudgetRefusesTheTOTPDisable is the other half: a
// settings action spends the re-auth budget, refuses the correct password once
// it is spent, and the 2FA disable refuses the correct password too, with its
// own rate-limit answer, leaving 2FA on.
func TestSettingsReauthSpentBudgetRefusesTheTOTPDisable(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "settings-reauth-budget-shared@example.com")
	enableTOTPForSettingsTest(t, &ctx)

	validate := func(password string) *http.Response {
		return settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe/validate", url.Values{
			"password": {password},
		}, map[string]string{"Accept": "application/json"})
	}
	for attempt := range services.DefaultSettingsReauthAttemptsLimit {
		if resp := validate("WrongPassword1"); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("clear-data validate, wrong password %d: status = %d, want 401", attempt+1, resp.StatusCode)
		}
	}
	if resp := validate("StrongPass1"); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("clear-data validate, correct password after the budget: status = %d, want 429", resp.StatusCode)
	}

	resp := sendDisableTOTP(t, ctx, "StrongPass1")
	assertDisableTOTPRefused(t, resp, http.StatusTooManyRequests, disableTOTPRateLimitedKey, "2FA disable after the settings re-auth budget was spent")
	if !totpEnabledInDatabase(t, ctx) {
		t.Fatal("a 2FA disable on a spent re-auth budget turned 2FA off")
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

// TestDisableTOTP2FARefusedWriteDoesNotResetTheDisableBudget pins the order of
// the reset against the write: a correct password whose DisableTOTP is refused
// (a revocation bumped auth_session_version after the request loaded the user)
// must leave the totp.disable count where it was. A callback on the disable's
// own UPDATE bumps the version inside its transaction, so the compare-and-set
// fails exactly once, after the password check has passed, and the rollback
// takes the bump with it.
func TestDisableTOTP2FARefusedWriteDoesNotResetTheDisableBudget(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-refused-write@example.com")
	enableTOTPForSettingsTest(t, &ctx)

	for attempt := range services.DefaultTOTPDisableAttemptsLimit - 1 {
		resp := sendDisableTOTP(t, ctx, "WrongPassword1")
		assertDisableTOTPRefused(t, resp, http.StatusUnauthorized, disableTOTPInvalidKey, "wrong password, attempt "+strconv.Itoa(attempt+1))
	}

	const revokeBeforeDisable = "test:totp-disable-revoke-before-cas"
	var revoked atomic.Bool
	if err := ctx.database.Callback().Update().Before("gorm:update").Register(revokeBeforeDisable, func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		if _, disablesTOTP := updates["totp_enabled"]; !disablesTOTP || !revoked.CompareAndSwap(false, true) {
			return
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE users SET auth_session_version = auth_session_version + 1 WHERE id = ?", ctx.user.ID).Error; err != nil {
			t.Errorf("simulate the mid-request revocation: %v", err)
		}
	}); err != nil {
		t.Fatalf("register the revocation callback: %v", err)
	}
	t.Cleanup(func() { _ = ctx.database.Callback().Update().Remove(revokeBeforeDisable) })

	resp := sendDisableTOTP(t, ctx, "StrongPass1")
	if !revoked.Load() {
		t.Fatal("anchor: the correct-password request never reached DisableTOTP's write")
	}
	assertDisableTOTPRefused(t, resp, http.StatusInternalServerError, "failed to create session", "the compare-and-set refusal arm")
	if !totpEnabledInDatabase(t, ctx) {
		t.Fatal("a disable whose compare-and-set failed still turned 2FA off")
	}

	// The refusal cleared the auth cookie; sign the same owner back in at the
	// version it kept, from the same client address.
	ctx.refreshAuthCookie(t)

	// One failure short of the limit was spent before the refused write. If that
	// write reset the count, this correct password would be accepted below.
	resp = sendDisableTOTP(t, ctx, "WrongPassword1")
	assertDisableTOTPRefused(t, resp, http.StatusUnauthorized, disableTOTPInvalidKey, "the failure that reaches the limit")
	resp = sendDisableTOTP(t, ctx, "StrongPass1")
	assertDisableTOTPRefused(t, resp, http.StatusTooManyRequests, disableTOTPRateLimitedKey, "correct password after the refused write kept the count")
	if !totpEnabledInDatabase(t, ctx) {
		t.Fatal("a rate-limited disable turned 2FA off")
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
