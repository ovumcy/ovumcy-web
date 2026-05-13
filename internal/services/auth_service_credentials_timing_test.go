package services

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// TestAuthenticateCredentialsEqualizesTimingForMissingUser guards against the
// account-existence timing oracle: every "invalid creds" path must still pay
// the bcrypt cost. A bare early return without bcrypt would let an attacker
// distinguish "no such email" (~1ms) from "wrong password for existing user"
// (~50ms+). Threshold is generous (15ms) because bcrypt.DefaultCost is 10 on
// every supported platform — well above 30ms wall time even on slow CI.
func TestAuthenticateCredentialsEqualizesTimingForMissingUser(t *testing.T) {
	service := NewAuthService(&stubAuthUserRepo{
		findByEmailErr: errors.New("not found"),
	})

	start := time.Now()
	_, err := service.AuthenticateCredentials("nonexistent@example.com", "AnyPass1!")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds, got %v", err)
	}
	if elapsed < 15*time.Millisecond {
		t.Fatalf("nonexistent-user auth returned too quickly (%v) — bcrypt equalization is missing", elapsed)
	}
}

func TestAuthenticateCredentialsEqualizesTimingForDisabledLocalAuth(t *testing.T) {
	service := NewAuthService(&stubAuthUserRepo{
		findByEmailUser: models.User{
			LocalAuthEnabled: false,
			PasswordHash:     "",
		},
	})

	start := time.Now()
	_, err := service.AuthenticateCredentials("oidc-only@example.com", "AnyPass1!")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds, got %v", err)
	}
	if elapsed < 15*time.Millisecond {
		t.Fatalf("oidc-only auth returned too quickly (%v) — bcrypt equalization is missing", elapsed)
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
