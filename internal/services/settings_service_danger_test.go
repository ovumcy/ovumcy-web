package services

import (
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// TestVerifyReauthPasswordRefusesCorrectPasswordOnceBudgetSpent is the core of
// the re-auth budget: the erasure gate must not be a faster password oracle than
// the login form. Once the budget is spent the CORRECT password is refused too —
// that is what makes it a budget rather than a speed bump, and it mirrors the
// login-budget contract asserted by the OIDC link-confirm regression.
func TestVerifyReauthPasswordRefusesCorrectPasswordOnceBudgetSpent(t *testing.T) {
	service := NewSettingsService(nil)
	service.ConfigureReauthAttempts([]byte("test-secret"), NewAttemptLimiter(), 3, time.Minute)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	attempt := ReauthAttempt{ClientKey: "203.0.113.10", UserID: 42, Now: now}

	for i := 1; i <= 3; i++ {
		if err := service.VerifyReauthPassword(attempt, string(passwordHash), "WrongPass1"); !errors.Is(err, ErrSettingsPasswordInvalid) {
			t.Fatalf("wrong-password attempt %d: got %v, want ErrSettingsPasswordInvalid", i, err)
		}
	}

	if err := service.VerifyReauthPassword(attempt, string(passwordHash), "StrongPass1"); !errors.Is(err, ErrSettingsReauthRateLimited) {
		t.Fatalf("correct password after the budget was spent: got %v, want ErrSettingsReauthRateLimited", err)
	}

	// A different account from the same address keeps its own budget: the
	// per-account bucket must not turn one owner's failures into a lockout for
	// another owner sharing the address (household self-hosting behind one NAT).
	other := ReauthAttempt{ClientKey: "203.0.113.10", UserID: 43, Now: now}
	if err := service.VerifyReauthPassword(other, string(passwordHash), "StrongPass1"); err != nil {
		t.Fatalf("second account on the same address: got %v, want success", err)
	}
}

// TestVerifyReauthPasswordSuccessResetsBudget proves failures below the limit do
// not accumulate forever: a correct password clears the counter, so ordinary
// mistyping never walks an owner into a lockout.
func TestVerifyReauthPasswordSuccessResetsBudget(t *testing.T) {
	service := NewSettingsService(nil)
	service.ConfigureReauthAttempts([]byte("test-secret"), NewAttemptLimiter(), 3, time.Minute)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	attempt := ReauthAttempt{ClientKey: "203.0.113.10", UserID: 42, Now: now}

	for i := 1; i <= 2; i++ {
		if err := service.VerifyReauthPassword(attempt, string(passwordHash), "WrongPass1"); !errors.Is(err, ErrSettingsPasswordInvalid) {
			t.Fatalf("wrong-password attempt %d: got %v", i, err)
		}
	}
	if err := service.VerifyReauthPassword(attempt, string(passwordHash), "StrongPass1"); err != nil {
		t.Fatalf("correct password within budget: got %v, want success", err)
	}
	// Counter cleared: the full budget is available again.
	for i := 1; i <= 3; i++ {
		if err := service.VerifyReauthPassword(attempt, string(passwordHash), "WrongPass1"); !errors.Is(err, ErrSettingsPasswordInvalid) {
			t.Fatalf("post-reset attempt %d: got %v, want ErrSettingsPasswordInvalid (budget was not reset)", i, err)
		}
	}
}

// TestVerifyReauthPasswordDoesNotSpendBudgetOnBlankSubmission keeps the budget
// aimed at guesses. A blank field is a client mistake, and counting it would let
// a stray double-submit walk an owner toward a lockout.
func TestVerifyReauthPasswordDoesNotSpendBudgetOnBlankSubmission(t *testing.T) {
	service := NewSettingsService(nil)
	service.ConfigureReauthAttempts([]byte("test-secret"), NewAttemptLimiter(), 2, time.Minute)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	attempt := ReauthAttempt{ClientKey: "203.0.113.10", UserID: 42, Now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}

	for i := 1; i <= 5; i++ {
		if err := service.VerifyReauthPassword(attempt, string(passwordHash), "   "); !errors.Is(err, ErrSettingsPasswordMissing) {
			t.Fatalf("blank submission %d: got %v, want ErrSettingsPasswordMissing", i, err)
		}
	}
	if err := service.VerifyReauthPassword(attempt, string(passwordHash), "StrongPass1"); err != nil {
		t.Fatalf("correct password after blank submissions: got %v, want success", err)
	}
}

func TestValidateCurrentPasswordRejectsMissingPassword(t *testing.T) {
	service := NewSettingsService(nil)

	err := service.ValidateCurrentPassword("ignored", "   ")
	if !errors.Is(err, ErrSettingsPasswordMissing) {
		t.Fatalf("expected ErrSettingsPasswordMissing, got %v", err)
	}
}

func TestValidateCurrentPasswordRejectsInvalidPassword(t *testing.T) {
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	err = service.ValidateCurrentPassword(string(passwordHash), "WrongPass1")
	if !errors.Is(err, ErrSettingsPasswordInvalid) {
		t.Fatalf("expected ErrSettingsPasswordInvalid, got %v", err)
	}
}

func TestValidateCurrentPasswordAcceptsMatchingPassword(t *testing.T) {
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if err := service.ValidateCurrentPassword(string(passwordHash), "  StrongPass1  "); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}
