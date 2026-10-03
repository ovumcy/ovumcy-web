package services

import (
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// enrollBudget is the totp.enroll budget on the fixture's shared limiter.
func (fixture reauthBudgetFixture) enrollBudget() ReauthBudget {
	return fixture.totp.EnrollCodeBudget([]byte("reauth-budget-routing-secret-32b!"))
}

func enrollmentCodesForTest(t *testing.T, svc *TOTPService) (secret string, wrongCode string, validCode func() string) {
	t.Helper()
	key, err := svc.GenerateSetupKey("Ovumcy", "enroll-budget@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	secret = key.Secret()
	return secret, invalidTOTPCodeForSkewWindow(t, secret), func() string {
		code, err := totp.GenerateCode(secret, time.Now())
		if err != nil {
			t.Fatalf("GenerateCode: %v", err)
		}
		return code
	}
}

// TestVerifyEnrollmentCodeBooksAWrongCodeAndRefusesTheCorrectOneOnceSpent pins
// the totp.enroll budget: each wrong code answers ErrTOTPEnrollCodeInvalid and
// books one failure, the limit's worth exhausts it, and then the correct code is
// refused ErrTOTPEnrollRateLimited before it is checked. A correct code under
// the limit never clears the count; only Reset does.
func TestVerifyEnrollmentCodeBooksAWrongCodeAndRefusesTheCorrectOneOnceSpent(t *testing.T) {
	fixture := newReauthBudgetFixture(t)
	secret, wrongCode, validCode := enrollmentCodesForTest(t, fixture.totp)
	budget := fixture.enrollBudget()

	for attempt := range DefaultTOTPEnrollAttemptsLimit - 1 {
		if _, err := fixture.totp.VerifyEnrollmentCode(budget, fixture.attempt, secret, wrongCode); !errors.Is(err, ErrTOTPEnrollCodeInvalid) {
			t.Fatalf("wrong code, attempt %d = %v, want ErrTOTPEnrollCodeInvalid", attempt+1, err)
		}
	}
	if _, err := fixture.totp.VerifyEnrollmentCode(budget, fixture.attempt, secret, validCode()); err != nil {
		t.Fatalf("correct code one short of the limit = %v, want nil", err)
	}
	if _, err := fixture.totp.VerifyEnrollmentCode(budget, fixture.attempt, secret, wrongCode); !errors.Is(err, ErrTOTPEnrollCodeInvalid) {
		t.Fatalf("wrong code reaching the limit = %v, want ErrTOTPEnrollCodeInvalid", err)
	}
	if _, err := fixture.totp.VerifyEnrollmentCode(budget, fixture.attempt, secret, validCode()); !errors.Is(err, ErrTOTPEnrollRateLimited) {
		t.Fatalf("correct code on the spent budget = %v, want ErrTOTPEnrollRateLimited: a correct code cleared the count, or the budget is not checked first", err)
	}

	budget.Reset(fixture.attempt)
	if _, err := fixture.totp.VerifyEnrollmentCode(budget, fixture.attempt, secret, validCode()); err != nil {
		t.Fatalf("correct code after Reset = %v, want nil", err)
	}
}

// TestTOTPEnrollBudgetIsItsOwnScopeAndPerAccount pins the keying the budget
// shares with the password re-auth budget: spending totp.enroll leaves
// settings.reauth (the settings actions and the 2FA disable) undrawn on the
// same limiter, and leaves another account behind the same address its whole
// budget.
func TestTOTPEnrollBudgetIsItsOwnScopeAndPerAccount(t *testing.T) {
	fixture := newReauthBudgetFixture(t)
	secret, wrongCode, validCode := enrollmentCodesForTest(t, fixture.totp)
	budget := fixture.enrollBudget()
	for range DefaultTOTPEnrollAttemptsLimit {
		_, _ = fixture.totp.VerifyEnrollmentCode(budget, fixture.attempt, secret, wrongCode)
	}
	if _, err := fixture.totp.VerifyEnrollmentCode(budget, fixture.attempt, secret, validCode()); !errors.Is(err, ErrTOTPEnrollRateLimited) {
		t.Fatalf("anchor: correct code on the spent account = %v, want ErrTOTPEnrollRateLimited", err)
	}

	if err := fixture.settings.VerifyReauth(fixture.settings.SettingsReauthBudget(), fixture.attempt, fixture.user, reauthBudgetFixturePassword); err != nil {
		t.Fatalf("settings.reauth after spending totp.enroll = %v, want nil", err)
	}
	if err := fixture.settings.VerifyReauth(fixture.disableBudget(), fixture.attempt, fixture.user, reauthBudgetFixturePassword); err != nil {
		t.Fatalf("2FA disable after spending totp.enroll = %v, want nil", err)
	}

	neighbour := fixture.attempt
	neighbour.UserID = fixture.attempt.UserID + 1
	if _, err := fixture.totp.VerifyEnrollmentCode(budget, neighbour, secret, validCode()); err != nil {
		t.Fatalf("correct code for another account on the same address = %v, want nil", err)
	}
}

// TestTOTPEnrollBudgetHoldsAcrossClientAddresses pins the account-wide bucket:
// a budget spent from one address still refuses the correct code from a fresh
// one, so rotating addresses buys an attacker nothing.
func TestTOTPEnrollBudgetHoldsAcrossClientAddresses(t *testing.T) {
	fixture := newReauthBudgetFixture(t)
	secret, wrongCode, validCode := enrollmentCodesForTest(t, fixture.totp)
	budget := fixture.enrollBudget()
	for range DefaultTOTPEnrollAttemptsLimit {
		_, _ = fixture.totp.VerifyEnrollmentCode(budget, fixture.attempt, secret, wrongCode)
	}

	rotated := fixture.attempt
	rotated.ClientKey = "203.0.113.99"
	if _, err := fixture.totp.VerifyEnrollmentCode(budget, rotated, secret, validCode()); !errors.Is(err, ErrTOTPEnrollRateLimited) {
		t.Fatalf("correct code from a fresh address on the spent account = %v, want ErrTOTPEnrollRateLimited", err)
	}
}

// TestTOTPEnrollBudgetIsUndrawnBySettingsReauth is the reverse routing check:
// a spent settings.reauth budget leaves the enrollment code its whole budget.
func TestTOTPEnrollBudgetIsUndrawnBySettingsReauth(t *testing.T) {
	fixture := newReauthBudgetFixture(t)
	secret, _, validCode := enrollmentCodesForTest(t, fixture.totp)
	fixture.spend(t, fixture.settings.SettingsReauthBudget(), DefaultSettingsReauthAttemptsLimit)

	if _, err := fixture.totp.VerifyEnrollmentCode(fixture.enrollBudget(), fixture.attempt, secret, validCode()); err != nil {
		t.Fatalf("correct code after spending settings.reauth = %v, want nil", err)
	}
}

// TestConfigureEnrollAttemptsSetsTheWindow pins the configure hook bootstrap
// and the transport tests use: a failure older than the configured window no
// longer counts, one inside it still does.
func TestConfigureEnrollAttemptsSetsTheWindow(t *testing.T) {
	fixture := newReauthBudgetFixture(t)
	fixture.totp.ConfigureEnrollAttempts(DefaultTOTPEnrollAttemptsLimit, time.Minute)
	secret, wrongCode, validCode := enrollmentCodesForTest(t, fixture.totp)
	budget := fixture.enrollBudget()

	stale := fixture.attempt
	stale.Now = fixture.attempt.Now.Add(-2 * time.Minute)
	for range DefaultTOTPEnrollAttemptsLimit {
		_, _ = fixture.totp.VerifyEnrollmentCode(budget, stale, secret, wrongCode)
	}
	if _, err := fixture.totp.VerifyEnrollmentCode(budget, fixture.attempt, secret, validCode()); err != nil {
		t.Fatalf("correct code with every failure outside the one-minute window = %v, want nil", err)
	}

	recent := fixture.attempt
	recent.Now = fixture.attempt.Now.Add(-30 * time.Second)
	for range DefaultTOTPEnrollAttemptsLimit {
		_, _ = fixture.totp.VerifyEnrollmentCode(budget, recent, secret, wrongCode)
	}
	if _, err := fixture.totp.VerifyEnrollmentCode(budget, fixture.attempt, secret, validCode()); !errors.Is(err, ErrTOTPEnrollRateLimited) {
		t.Fatalf("correct code with the limit's failures inside the window = %v, want ErrTOTPEnrollRateLimited", err)
	}
}
