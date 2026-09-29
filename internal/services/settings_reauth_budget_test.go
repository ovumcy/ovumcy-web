package services

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// The totp.disable budget is exercised through the same verify every re-auth
// uses; these three helpers only spell its admission, booking and reset in the
// shape the rate-limit tests were written against.

func checkDisableBudget(svc *TOTPService, secretKey []byte, clientKey string, userID uint, now time.Time) error {
	attempt := ReauthAttempt{ClientKey: clientKey, UserID: userID, Now: now}
	return svc.DisableReauthBudget(secretKey).verify(attempt, func() error { return nil })
}

func recordDisableFailure(svc *TOTPService, secretKey []byte, clientKey string, userID uint, now time.Time) {
	attempt := ReauthAttempt{ClientKey: clientKey, UserID: userID, Now: now}
	svc.DisableReauthBudget(secretKey).bookFailure(attempt)
}

func resetDisableBudget(svc *TOTPService, secretKey []byte, clientKey string, userID uint) {
	svc.DisableReauthBudget(secretKey).Reset(ReauthAttempt{ClientKey: clientKey, UserID: userID})
}

type reauthBudgetFixture struct {
	settings *SettingsService
	totp     *TOTPService
	user     *models.User
	attempt  ReauthAttempt
}

const reauthBudgetFixturePassword = "StrongPass1"

// newReauthBudgetFixture wires both budgets onto ONE shared limiter and one key,
// the way bootstrap does, so a draw that landed in the wrong scope would show.
func newReauthBudgetFixture(t *testing.T) reauthBudgetFixture {
	t.Helper()
	secretKey := []byte("reauth-budget-routing-secret-32b!")
	limiter := NewAttemptLimiter()
	settings := NewSettingsService(nil)
	settings.ConfigureReauthAttempts(secretKey, limiter, DefaultSettingsReauthAttemptsLimit, DefaultSettingsReauthAttemptsWindow)
	hash, err := bcrypt.GenerateFromPassword([]byte(reauthBudgetFixturePassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash the fixture password: %v", err)
	}
	return reauthBudgetFixture{
		settings: settings,
		totp:     NewTOTPService(&stubTOTPUserRepo{}, secretKey, limiter),
		user:     &models.User{ID: 42, PasswordHash: string(hash), LocalAuthEnabled: true},
		attempt:  ReauthAttempt{ClientKey: "198.51.100.7", UserID: 42, Now: time.Now()},
	}
}

func (fixture reauthBudgetFixture) disableBudget() ReauthBudget {
	return fixture.totp.DisableReauthBudget([]byte("reauth-budget-routing-secret-32b!"))
}

func (fixture reauthBudgetFixture) spend(t *testing.T, budget ReauthBudget, limit int) {
	t.Helper()
	for range limit {
		if err := fixture.settings.VerifyReauth(budget, fixture.attempt, fixture.user, "WrongPassword1"); !errors.Is(err, ErrSettingsPasswordInvalid) {
			t.Fatalf("wrong password = %v, want ErrSettingsPasswordInvalid", err)
		}
	}
}

// TestVerifyReauthDrawsOnlyTheBudgetItIsGiven pins the helper's routing: the
// totp.disable budget's failures never reach settings.reauth, and the reverse,
// on one shared limiter.
func TestVerifyReauthDrawsOnlyTheBudgetItIsGiven(t *testing.T) {
	t.Run("totp.disable", func(t *testing.T) {
		fixture := newReauthBudgetFixture(t)
		fixture.spend(t, fixture.disableBudget(), DefaultTOTPDisableAttemptsLimit)

		if err := fixture.settings.VerifyReauth(fixture.disableBudget(), fixture.attempt, fixture.user, reauthBudgetFixturePassword); !errors.Is(err, ErrTOTPDisableRateLimited) {
			t.Fatalf("correct password on the spent totp.disable budget = %v, want ErrTOTPDisableRateLimited", err)
		}
		if err := fixture.settings.VerifyReauth(fixture.settings.SettingsReauthBudget(), fixture.attempt, fixture.user, reauthBudgetFixturePassword); err != nil {
			t.Fatalf("correct password on settings.reauth after spending totp.disable = %v, want nil", err)
		}
	})
	t.Run("settings.reauth", func(t *testing.T) {
		fixture := newReauthBudgetFixture(t)
		fixture.spend(t, fixture.settings.SettingsReauthBudget(), DefaultSettingsReauthAttemptsLimit)

		if err := fixture.settings.VerifyReauth(fixture.settings.SettingsReauthBudget(), fixture.attempt, fixture.user, reauthBudgetFixturePassword); !errors.Is(err, ErrSettingsReauthRateLimited) {
			t.Fatalf("correct password on the spent settings.reauth budget = %v, want ErrSettingsReauthRateLimited", err)
		}
		if err := fixture.settings.VerifyReauth(fixture.disableBudget(), fixture.attempt, fixture.user, reauthBudgetFixturePassword); err != nil {
			t.Fatalf("correct password on totp.disable after spending settings.reauth = %v, want nil", err)
		}
	})
}

// TestVerifyReauthLeavesTheResetToTheCaller pins the split between the verify
// step and the reset: a correct password through VerifyReauth keeps the count it
// found, and only budget.Reset clears it. VerifyReauthPassword, the settings.reauth
// composition, still resets at once.
func TestVerifyReauthLeavesTheResetToTheCaller(t *testing.T) {
	for _, tc := range []struct {
		name   string
		budget func(reauthBudgetFixture) ReauthBudget
		limit  int
	}{
		{"totp.disable", reauthBudgetFixture.disableBudget, DefaultTOTPDisableAttemptsLimit},
		{"settings.reauth", func(fixture reauthBudgetFixture) ReauthBudget { return fixture.settings.SettingsReauthBudget() }, DefaultSettingsReauthAttemptsLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newReauthBudgetFixture(t)
			budget := tc.budget(fixture)
			fixture.spend(t, budget, tc.limit-1)
			if err := fixture.settings.VerifyReauth(budget, fixture.attempt, fixture.user, reauthBudgetFixturePassword); err != nil {
				t.Fatalf("correct password one short of the limit = %v, want nil", err)
			}
			fixture.spend(t, budget, 1)
			if err := fixture.settings.VerifyReauth(budget, fixture.attempt, fixture.user, reauthBudgetFixturePassword); err == nil {
				t.Fatal("a successful verify cleared the count: the reset is the caller's step")
			}
			budget.Reset(fixture.attempt)
			if err := fixture.settings.VerifyReauth(budget, fixture.attempt, fixture.user, reauthBudgetFixturePassword); err != nil {
				t.Fatalf("correct password after Reset = %v, want nil", err)
			}
		})
	}

	fixture := newReauthBudgetFixture(t)
	fixture.spend(t, fixture.settings.SettingsReauthBudget(), DefaultSettingsReauthAttemptsLimit-1)
	if err := fixture.settings.VerifyReauthPassword(fixture.attempt, fixture.user, reauthBudgetFixturePassword); err != nil {
		t.Fatalf("VerifyReauthPassword one short of the limit = %v, want nil", err)
	}
	fixture.spend(t, fixture.settings.SettingsReauthBudget(), DefaultSettingsReauthAttemptsLimit-1)
	if err := fixture.settings.VerifyReauthPassword(fixture.attempt, fixture.user, reauthBudgetFixturePassword); err != nil {
		t.Fatalf("VerifyReauthPassword after a reset and %d fresh failures = %v, want nil", DefaultSettingsReauthAttemptsLimit-1, err)
	}
}

// TestVerifyReauthTrimsAndLeavesABlankSubmissionUncounted pins the trim and the
// blank refusal every budget shares: surrounding whitespace is not part of the
// password, and a blank one is refused without drawing either budget.
func TestVerifyReauthTrimsAndLeavesABlankSubmissionUncounted(t *testing.T) {
	fixture := newReauthBudgetFixture(t)
	for _, budget := range []ReauthBudget{fixture.disableBudget(), fixture.settings.SettingsReauthBudget()} {
		for range DefaultSettingsReauthAttemptsLimit + 1 {
			if err := fixture.settings.VerifyReauth(budget, fixture.attempt, fixture.user, " \t "); !errors.Is(err, ErrSettingsPasswordMissing) {
				t.Fatalf("blank password = %v, want ErrSettingsPasswordMissing", err)
			}
		}
		if err := fixture.settings.VerifyReauth(budget, fixture.attempt, fixture.user, "  "+reauthBudgetFixturePassword+"\t"); err != nil {
			t.Fatalf("correct password with surrounding whitespace after blanks = %v, want nil", err)
		}
	}
}
