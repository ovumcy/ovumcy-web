package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// These tests guard against the account-existence timing oracle: every
// "invalid creds" early-return path in AuthenticateCredentials must still
// invoke the bcrypt equalizer so an attacker cannot distinguish
// "no such email" (~1ms early return) from "wrong password for existing
// user" (~50ms+).
//
// The earlier wall-clock budget (`elapsed >= 15*time.Millisecond`) was
// replaced with a call-counter wrapper to remove wall-clock fragility on
// shared CI runners. The counter asserts the equalizer was invoked exactly
// once per call; that is the actual invariant we care about. A separate
// test (TestCredentialsTimingEqualizationHashIsBcryptCompatible) still
// verifies the placeholder hash is a real bcrypt hash, so the equalizer
// cannot silently short-circuit even when overridden in tests.

func withCountingCredentialsEqualizer(t *testing.T) *int {
	t.Helper()

	original := equalizeAuthCredentialsTiming
	count := 0
	equalizeAuthCredentialsTiming = func(string) {
		count++
	}
	t.Cleanup(func() {
		equalizeAuthCredentialsTiming = original
	})
	return &count
}

func TestAuthenticateCredentialsEqualizesTimingForMissingUser(t *testing.T) {
	count := withCountingCredentialsEqualizer(t)
	service := NewAuthService(&stubAuthUserRepo{
		findByEmailErr: errors.New("not found"),
	})

	_, err := service.AuthenticateCredentials(context.Background(), "nonexistent@example.com", "AnyPass1!")

	if !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds, got %v", err)
	}
	if *count != 1 {
		t.Fatalf("expected exactly 1 bcrypt equalization call on missing-user path, got %d", *count)
	}
}

func TestAuthenticateCredentialsEqualizesTimingForDisabledLocalAuth(t *testing.T) {
	count := withCountingCredentialsEqualizer(t)
	service := NewAuthService(&stubAuthUserRepo{
		findByEmailUser: models.User{
			LocalAuthEnabled: false,
			PasswordHash:     "",
		},
	})

	_, err := service.AuthenticateCredentials(context.Background(), "oidc-only@example.com", "AnyPass1!")

	if !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds, got %v", err)
	}
	if *count != 1 {
		t.Fatalf("expected exactly 1 bcrypt equalization call on oidc-only path, got %d", *count)
	}
}

// TestCredentialsTimingEqualizationHashIsBcryptCompatible ensures the constant
// is never silently corrupted into an unparseable value, which would make the
// equalizer return instantly and reintroduce the timing oracle.
func TestCredentialsTimingEqualizationHashIsBcryptCompatible(t *testing.T) {
	if err := bcrypt.CompareHashAndPassword([]byte(credentialsTimingEqualizationHash), []byte("any")); err == nil {
		t.Fatal("hash unexpectedly matched 'any' — wrong placeholder?")
	} else if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatalf("hash is unparseable by bcrypt (%v) — equalizer would short-circuit", err)
	}
}
