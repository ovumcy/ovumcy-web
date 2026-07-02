package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// These tests are the registration-side mirror of the login timing guards in
// auth_service_credentials_timing_test.go. The duplicate-email branch of
// RegisterOwner must spend the same bcrypt work as a fresh registration, so a
// POST /api/v1/users probe cannot distinguish a new email from an existing one
// by response latency (SECURITY.md "Register enumeration: pickup-cookie
// follow-up oracle"). Without this guard a refactor could drop the equalize
// call on the early-return path and silently downgrade the documented
// two-request oracle into a stronger one-request timing oracle, with no other
// test failing. As with the login guards, the placeholder hash is checked
// separately so the equalizer cannot short-circuit even when overridden here.

func TestRegisterOwnerEqualizesTimingForDuplicateEmail(t *testing.T) {
	original := equalizeRegistrationTiming
	count := 0
	var gotPassword string
	equalizeRegistrationTiming = func(password string) {
		count++
		gotPassword = password
	}
	t.Cleanup(func() {
		equalizeRegistrationTiming = original
	})

	service := NewAuthService(&stubAuthUserRepo{existsByEmail: true})

	_, _, err := service.RegisterOwner(context.Background(), "taken@example.com", "StrongPass1", "StrongPass1", time.Time{})

	if !errors.Is(err, ErrAuthEmailExists) {
		t.Fatalf("expected ErrAuthEmailExists on duplicate email, got %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 registration timing equalization call on duplicate-email path, got %d", count)
	}
	if gotPassword != "StrongPass1" {
		t.Fatalf("expected the equalizer to run against the submitted password, got %q", gotPassword)
	}
}

// TestRecoveryCodeTimingEqualizationHashIsBcryptCompatible mirrors the
// credentials-hash check for the second placeholder. recoveryCodeTimingEqualizationHash
// is the hash equalizeRegistrationTiming (and equalizeRecoveryCodeLookupTiming)
// spend the second bcrypt comparison against; if it is ever corrupted into an
// unparseable value that comparison returns instantly and the equalizer stops
// closing the timing gap.
func TestRecoveryCodeTimingEqualizationHashIsBcryptCompatible(t *testing.T) {
	if err := bcrypt.CompareHashAndPassword([]byte(recoveryCodeTimingEqualizationHash), []byte("any")); err == nil {
		t.Fatal("hash unexpectedly matched 'any' — wrong placeholder?")
	} else if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatalf("hash is unparseable by bcrypt (%v) — equalizer would short-circuit", err)
	}
}
