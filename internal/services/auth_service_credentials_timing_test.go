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
// once per call; that is half the invariant.
//
// The other half is that the equalizer still SPENDS something, and neither the
// counter nor TestCredentialsTimingEqualizationHashIsBcryptCompatible can see
// it: the counter replaces the whole var, and the hash check reads the constant
// without ever asking whether the body still compares against it. An emptied
// body left both of them green while the unknown-address branch returned with
// no bcrypt work at all. TestAuthCredentialsEqualizerBodyComparesThePlaceholder
// below closes that by swapping the compare INSIDE the body — the same seam
// discipline timingTopUpCompare already gives the top-up half.

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

// equalizerCompare is one comparison the shipped equalizer body made, read off
// the seam rather than off the constant the body is supposed to name.
type equalizerCompare struct {
	hash    string
	operand string
}

// withEqualizerCompareRecorder records every comparison an equalizer body
// spends and still calls the production compare, so the body under test runs
// exactly as it ships and only the accounting is added.
func withEqualizerCompareRecorder(t *testing.T) *[]equalizerCompare {
	t.Helper()

	original := authTimingEqualizerCompare
	recorded := []equalizerCompare{}
	authTimingEqualizerCompare = func(hash []byte, operand []byte) error {
		recorded = append(recorded, equalizerCompare{hash: string(hash), operand: string(operand)})
		return original(hash, operand)
	}
	t.Cleanup(func() { authTimingEqualizerCompare = original })
	return &recorded
}

// assertEqualizerSpent is the shared assertion for the two body tests: the
// comparisons the body actually made, in order, against the placeholders it is
// supposed to name and the operand it was handed. An empty body, a body that
// drops one of two comparisons, and a body that compares against the wrong
// placeholder are three different mutants and each fails here.
func assertEqualizerSpent(t *testing.T, recorded []equalizerCompare, wantHashes []string, wantOperand string) {
	t.Helper()

	if len(recorded) != len(wantHashes) {
		t.Fatalf("the equalizer body spent %d bcrypt comparisons, want %d — an equalizer that spends less than it claims is the timing oracle it exists to close",
			len(recorded), len(wantHashes))
	}
	for index, want := range wantHashes {
		if recorded[index].hash != want {
			t.Fatalf("comparison %d ran against a hash other than the placeholder it must name — the work it buys is then whatever that hash costs", index)
		}
		if recorded[index].operand != wantOperand {
			t.Fatalf("comparison %d ran against operand %q, want the submitted secret %q", index, recorded[index].operand, wantOperand)
		}
	}
}

// TestAuthCredentialsEqualizerBodyComparesThePlaceholder drives the SHIPPED
// equalizer body, which the two call-site tests above cannot: they swap the
// whole var away. Without this, emptying the body passes the entire suite —
// including the work ledger in auth_service_timing_cost_topup_test.go, which
// until this seam existed read the equalized branch's cost off
// credentialsTimingEqualizationHash instead of off the comparison.
func TestAuthCredentialsEqualizerBodyComparesThePlaceholder(t *testing.T) {
	const submittedPassword = "WrongGuess1!"
	recorded := withEqualizerCompareRecorder(t)

	equalizeAuthCredentialsTiming(submittedPassword)

	assertEqualizerSpent(t, *recorded, []string{credentialsTimingEqualizationHash}, submittedPassword)
}
