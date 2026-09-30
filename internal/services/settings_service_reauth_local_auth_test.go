package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// localPasswordUser is the account shape every settings re-auth test compares
// against: local sign-in on, with the given stored hash.
func localPasswordUser(passwordHash string) *models.User {
	return &models.User{ID: 42, PasswordHash: passwordHash, LocalAuthEnabled: true}
}

// signInDisabledUser holds a hash that matches correctPassword while
// local_auth_enabled is off: the state AuthenticateCredentials refuses at sign-in.
func signInDisabledUser(t *testing.T, correctPassword string) *models.User {
	t.Helper()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(correctPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return &models.User{ID: 42, PasswordHash: string(passwordHash), LocalAuthEnabled: false, AuthSessionVersion: 1}
}

// TestSettingsReauthRefusesAStoredHashWhileLocalSignInIsOff is WEB-112: every
// settings re-auth entry point must refuse the CORRECT password of an account
// whose local sign-in is off, exactly as it refuses an account with no hash —
// ErrSettingsLocalPasswordNotSet, through the equalized branch — rather than
// comparing against a hash sign-in itself will not accept.
func TestSettingsReauthRefusesAStoredHashWhileLocalSignInIsOff(t *testing.T) {
	const correctPassword = "StrongPass1"
	attempt := ReauthAttempt{ClientKey: "203.0.113.10", UserID: 42, Now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}

	cases := []struct {
		name  string
		check func(service *SettingsService, user *models.User) error
	}{
		{"ValidateCurrentPassword", func(service *SettingsService, user *models.User) error {
			return service.ValidateCurrentPassword(user, correctPassword)
		}},
		{"VerifyReauthPassword", func(service *SettingsService, user *models.User) error {
			_, err := service.VerifyReauthPassword(attempt, user, correctPassword)
			return err
		}},
		{"ValidatePasswordChange", func(service *SettingsService, user *models.User) error {
			return service.ValidatePasswordChange(user, correctPassword, "EvenStronger2", "EvenStronger2")
		}},
		{"ChangePassword", func(service *SettingsService, user *models.User) error {
			return service.ChangePassword(context.Background(), attempt, user, correctPassword, "EvenStronger2", "EvenStronger2")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := signInDisabledUser(t, correctPassword)

			// Anchor: the same row with the flag on is accepted, so the refusal
			// below is the flag's and not a wrong fixture password's.
			enabled := *user
			enabled.LocalAuthEnabled = true
			if err := tc.check(NewSettingsService(&stubSettingsUserRepo{}), &enabled); err != nil {
				t.Fatalf("anchor: correct password with local sign-in on: got %v, want nil", err)
			}

			calls := withCountingSettingsReauthEqualizer(t)
			repo := &stubSettingsUserRepo{}
			if err := tc.check(NewSettingsService(repo), user); !errors.Is(err, ErrSettingsLocalPasswordNotSet) {
				t.Fatalf("correct password with local sign-in off: got %v, want ErrSettingsLocalPasswordNotSet", err)
			}
			if *calls != 1 {
				t.Fatalf("equalizer ran %d times, want 1 — the refusal must cost what the empty-hash refusal costs", *calls)
			}
			if repo.updatePasswordCalled {
				t.Fatal("a refused re-auth reached the repository")
			}
		})
	}
}

// TestSettingsReauthRefusesANilUserThroughTheEqualizedBranch pins the
// fail-closed reading of a missing user: no hash to compare, same answer and
// same cost as an account without a local password.
func TestSettingsReauthRefusesANilUserThroughTheEqualizedBranch(t *testing.T) {
	calls := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	if err := service.ValidateCurrentPassword(nil, "StrongPass1"); !errors.Is(err, ErrSettingsLocalPasswordNotSet) {
		t.Fatalf("ValidateCurrentPassword(nil): got %v, want ErrSettingsLocalPasswordNotSet", err)
	}
	if err := service.ValidatePasswordChange(nil, "StrongPass1", "EvenStronger2", "EvenStronger2"); !errors.Is(err, ErrSettingsLocalPasswordNotSet) {
		t.Fatalf("ValidatePasswordChange(nil): got %v, want ErrSettingsLocalPasswordNotSet", err)
	}
	if *calls != 2 {
		t.Fatalf("equalizer ran %d times, want 2", *calls)
	}
}

// TestSettingsReauthSignInDisabledRefusalSpendsNothingOnBlankSubmission keeps
// the ordering the empty-hash branch relies on: a blank submission is answered
// before account state is read, so the flag cannot buy a bcrypt either.
func TestSettingsReauthSignInDisabledRefusalSpendsNothingOnBlankSubmission(t *testing.T) {
	calls := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)
	user := signInDisabledUser(t, "StrongPass1")

	if err := service.ValidateCurrentPassword(user, "   "); !errors.Is(err, ErrSettingsPasswordMissing) {
		t.Fatalf("ValidateCurrentPassword: got %v, want ErrSettingsPasswordMissing", err)
	}
	if err := service.ValidatePasswordChange(user, "   ", "EvenStronger2", "EvenStronger2"); !errors.Is(err, ErrSettingsPasswordChangeInvalidInput) {
		t.Fatalf("ValidatePasswordChange: got %v, want ErrSettingsPasswordChangeInvalidInput", err)
	}
	if *calls != 0 {
		t.Fatalf("equalizer ran %d times on blank submissions, want 0", *calls)
	}
}

// TestSettingsReauthSignInDisabledRefusalDrawsTheBudget matches the empty-hash
// rule (TestSettingsReauthNoLocalPasswordRefusalDrawsTheBudget): the refusal
// spent an equalized bcrypt, so it is booked against settings.reauth, and once
// the budget is spent the request is refused before any compare.
func TestSettingsReauthSignInDisabledRefusalDrawsTheBudget(t *testing.T) {
	const correctPassword = "StrongPass1"
	cases := []struct {
		name   string
		refuse func(service *SettingsService, attempt ReauthAttempt, user *models.User) error
	}{
		{"erasure", func(service *SettingsService, attempt ReauthAttempt, user *models.User) error {
			_, err := service.VerifyReauthPassword(attempt, user, correctPassword)
			return err
		}},
		{"password change", func(service *SettingsService, attempt ReauthAttempt, user *models.User) error {
			return service.ChangePassword(context.Background(), attempt, user, correctPassword, "EvenStronger2", "EvenStronger2")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := signInDisabledUser(t, correctPassword)
			calls := withCountingSettingsReauthEqualizer(t)
			service := NewSettingsService(&stubSettingsUserRepo{})
			service.ConfigureReauthAttempts([]byte("test-secret"), NewAttemptLimiter(), 2, time.Minute)
			attempt := ReauthAttempt{ClientKey: "203.0.113.10", UserID: user.ID, Now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}

			for i := 1; i <= 2; i++ {
				if err := tc.refuse(service, attempt, user); !errors.Is(err, ErrSettingsLocalPasswordNotSet) {
					t.Fatalf("refusal %d: got %v, want ErrSettingsLocalPasswordNotSet", i, err)
				}
			}
			if err := tc.refuse(service, attempt, user); !errors.Is(err, ErrSettingsReauthRateLimited) {
				t.Fatalf("after the budget: got %v, want ErrSettingsReauthRateLimited", err)
			}
			if *calls != 2 {
				t.Fatalf("equalizer ran %d times, want 2 — the rate-limited call must spend nothing", *calls)
			}
		})
	}
}
