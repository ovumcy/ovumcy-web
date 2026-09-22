package services

import (
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// These tests guard the re-auth timing oracle SEC-L3 closes: both
// ValidateCurrentPassword (the erasure gate, via VerifyReauthPassword) and
// ValidatePasswordChange (the password-change gate) must spend the same
// bcrypt-shaped work on every early-return path that used to skip the
// compare, so an attacker already holding a session cannot distinguish
// "account has no local password" / "blank field" from "wrong password" by
// response latency.
//
// Per the repo's timing-equalization test discipline, this uses a
// call-counter wrapper (package-level var swapped via t.Cleanup), never a
// wall-clock threshold. The placeholder hash's bcrypt-compatibility is
// already pinned by TestCredentialsTimingEqualizationHashIsBcryptCompatible
// in auth_service_credentials_timing_test.go — both equalizers share the same
// constant, so that test doubles as this one's compatibility proof.

func withCountingSettingsReauthEqualizer(t *testing.T) *int {
	t.Helper()

	original := equalizeSettingsReauthTiming
	count := 0
	equalizeSettingsReauthTiming = func(string) {
		count++
	}
	t.Cleanup(func() {
		equalizeSettingsReauthTiming = original
	})
	return &count
}

func TestValidateCurrentPasswordEqualizesTimingForNoLocalPassword(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	err := service.ValidateCurrentPassword("", "AnyPass1!")

	if !errors.Is(err, ErrSettingsLocalPasswordNotSet) {
		t.Fatalf("expected ErrSettingsLocalPasswordNotSet, got %v", err)
	}
	if *count != 1 {
		t.Fatalf("expected exactly 1 bcrypt equalization call on the no-local-password path, got %d", *count)
	}
}

func TestValidateCurrentPasswordEqualizesTimingForBlankSubmission(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	err := service.ValidateCurrentPassword("ignored-hash", "   ")

	if !errors.Is(err, ErrSettingsPasswordMissing) {
		t.Fatalf("expected ErrSettingsPasswordMissing, got %v", err)
	}
	if *count != 1 {
		t.Fatalf("expected exactly 1 bcrypt equalization call on the blank-submission path, got %d", *count)
	}
}

// TestValidateCurrentPasswordDoesNotEqualizeOnRealCompare pins the
// counter-based test to the branch it claims to cover: once a real hash and a
// non-blank password reach bcrypt.CompareHashAndPassword, the equalizer must
// NOT also run — the real compare already spends the cost, and double-spending
// it would mask a regression that removed the real compare entirely.
func TestValidateCurrentPasswordDoesNotEqualizeOnRealCompare(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if err := service.ValidateCurrentPassword(string(passwordHash), "WrongPass1"); !errors.Is(err, ErrSettingsPasswordInvalid) {
		t.Fatalf("expected ErrSettingsPasswordInvalid, got %v", err)
	}
	if *count != 0 {
		t.Fatalf("expected 0 equalization calls once the real compare runs, got %d", *count)
	}
}

func TestValidatePasswordChangeEqualizesTimingForInvalidInput(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	err := service.ValidatePasswordChange("hash", " ", "NewPass1", "NewPass1")

	if !errors.Is(err, ErrSettingsPasswordChangeInvalidInput) {
		t.Fatalf("expected ErrSettingsPasswordChangeInvalidInput, got %v", err)
	}
	if *count != 1 {
		t.Fatalf("expected exactly 1 bcrypt equalization call on the invalid-input path, got %d", *count)
	}
}

func TestValidatePasswordChangeEqualizesTimingForMismatch(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	err := service.ValidatePasswordChange("hash", "StrongPass1", "NewPass1", "OtherPass1")

	if !errors.Is(err, ErrSettingsPasswordMismatch) {
		t.Fatalf("expected ErrSettingsPasswordMismatch, got %v", err)
	}
	if *count != 1 {
		t.Fatalf("expected exactly 1 bcrypt equalization call on the mismatch path, got %d", *count)
	}
}

func TestValidatePasswordChangeEqualizesTimingForNoLocalPassword(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	err := service.ValidatePasswordChange("", "StrongPass1", "NewPass1", "NewPass1")

	if !errors.Is(err, ErrSettingsLocalPasswordNotSet) {
		t.Fatalf("expected ErrSettingsLocalPasswordNotSet, got %v", err)
	}
	if *count != 1 {
		t.Fatalf("expected exactly 1 bcrypt equalization call on the no-local-password path, got %d", *count)
	}
}
