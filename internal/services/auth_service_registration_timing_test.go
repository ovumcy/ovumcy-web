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
// test failing. As with the login guards, the call-site test below replaces the
// whole var, so it says nothing about what the body spends, and the placeholder
// check at the end of this file reads the constant without asking whether the
// body still compares against it. TestRegistrationEqualizerBodyComparesBothPlaceholders
// between them is what makes an emptied or halved body red — halved matters
// here, because this equalizer owes TWO comparisons and dropping one restores
// half the latency gap.

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

// TestRegistrationEqualizerBodyComparesBothPlaceholders drives the SHIPPED
// body of equalizeRegistrationTiming. The duplicate-email branch has to mirror
// BuildOwnerUserWithRecovery, which mints TWO bcrypt hashes, so a body that
// spends one comparison is not a smaller defect than a body that spends none —
// it leaves exactly the half-cost branch an enumerator reads.
func TestRegistrationEqualizerBodyComparesBothPlaceholders(t *testing.T) {
	const submittedPassword = "StrongPass1"
	recorded := withEqualizerCompareRecorder(t)

	equalizeRegistrationTiming(submittedPassword)

	assertEqualizerSpent(t, *recorded,
		[]string{credentialsTimingEqualizationHash, recoveryCodeTimingEqualizationHash},
		submittedPassword)
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
