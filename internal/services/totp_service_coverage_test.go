package services

// totp_service_coverage_test.go
//
// Behaviour tests targeting surviving mutants and previously uncovered lines
// in totp_service.go.  Every assertion is against observable outcomes (return
// values, persisted state, typed errors) — never log strings or markup.
//
// Prefix convention: all helpers/vars defined here start with "totpserviceCov"
// to avoid symbol collisions when merged with other agents' files.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const totpserviceCovSecretKey = "test-secret-key-32-bytes-padding!"

func totpserviceCovNewSvc(t *testing.T) (*TOTPService, *stubTOTPUserRepo) {
	t.Helper()
	repo := &stubTOTPUserRepo{}
	svc := NewTOTPService(repo, []byte(totpserviceCovSecretKey), nil)
	return svc, repo
}

// totpserviceCovEnroll creates a fresh TOTP key, calls EnableTOTP for userID,
// and returns the raw secret and the persisted encrypted ciphertext.
func totpserviceCovEnroll(t *testing.T, svc *TOTPService, repo *stubTOTPUserRepo, userID uint) (rawSecret, encryptedSecret string) {
	t.Helper()
	key, err := svc.GenerateSetupKey("Ovumcy", fmt.Sprintf("user%d@example.com", userID))
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	if err := svc.EnableTOTP(context.Background(), userID, key.Secret()); err != nil {
		t.Fatalf("EnableTOTP(%d): %v", userID, err)
	}
	return key.Secret(), repo.updatedSecret
}

// ---------------------------------------------------------------------------
// Line 187 – delta slice {0, -1, +1}: verify backward and forward clock skew
// ---------------------------------------------------------------------------

// TestTOTPService_findValidatedTOTPStep_BackwardSkew asserts that a code
// generated one step in the past (delta=-1, i.e. 30 s ago) is accepted.
// A mutant that removes -1 from the delta slice causes this test to fail.
func TestTOTPService_findValidatedTOTPStep_BackwardSkew(t *testing.T) {
	svc, repo := totpserviceCovNewSvc(t)
	rawSecret, encryptedSecret := totpserviceCovEnroll(t, svc, repo, 10)

	// Generate code for exactly one step in the past.
	pastTime := time.Now().Add(-totpStepSeconds * time.Second)
	pastCode, err := totp.GenerateCode(rawSecret, pastTime)
	if err != nil {
		t.Fatalf("GenerateCode (past): %v", err)
	}

	valid, err := svc.ValidateCode(context.Background(), 10, encryptedSecret, pastCode)
	if err != nil {
		t.Fatalf("ValidateCode with past-step code: %v", err)
	}
	if !valid {
		t.Error("ValidateCode rejected a code from one TOTP step in the past (backward clock skew not tolerated)")
	}
}

// TestTOTPService_findValidatedTOTPStep_ForwardSkew asserts that a code
// generated one step in the future (delta=+1, i.e. 30 s from now) is accepted.
// A mutant that removes +1 from the delta slice causes this test to fail.
func TestTOTPService_findValidatedTOTPStep_ForwardSkew(t *testing.T) {
	svc, repo := totpserviceCovNewSvc(t)
	rawSecret, encryptedSecret := totpserviceCovEnroll(t, svc, repo, 11)

	// Generate code for exactly one step in the future.
	futureTime := time.Now().Add(totpStepSeconds * time.Second)
	futureCode, err := totp.GenerateCode(rawSecret, futureTime)
	if err != nil {
		t.Fatalf("GenerateCode (future): %v", err)
	}

	valid, err := svc.ValidateCode(context.Background(), 11, encryptedSecret, futureCode)
	if err != nil {
		t.Fatalf("ValidateCode with future-step code: %v", err)
	}
	if !valid {
		t.Error("ValidateCode rejected a code from one TOTP step in the future (forward clock skew not tolerated)")
	}
}

// ---------------------------------------------------------------------------
// Line 188 – step := currentStep + delta: verify correct step value is claimed
// ---------------------------------------------------------------------------

// TestTOTPService_findValidatedTOTPStep_PastCodeClaimsPastStep asserts that
// when a code from the previous step is validated, the step number that gets
// claimed in the repository equals the previous step (not the current step).
// A mutant that drops +delta would claim currentStep regardless of which
// delta actually matched, allowing a replayed past-step code after a
// current-step code has already been consumed.
func TestTOTPService_findValidatedTOTPStep_PastCodeClaimsPastStep(t *testing.T) {
	svc, repo := totpserviceCovNewSvc(t)
	rawSecret, encryptedSecret := totpserviceCovEnroll(t, svc, repo, 20)

	now := time.Now()
	currentStep := now.Unix() / totpStepSeconds
	expectedStep := currentStep - 1

	pastCode, err := totp.GenerateCode(rawSecret, time.Unix(expectedStep*totpStepSeconds, 0))
	if err != nil {
		t.Fatalf("GenerateCode (past step %d): %v", expectedStep, err)
	}

	valid, err := svc.ValidateCode(context.Background(), 20, encryptedSecret, pastCode)
	if err != nil {
		t.Fatalf("ValidateCode with past-step code: %v", err)
	}
	if !valid {
		t.Fatal("ValidateCode rejected a past-step code — backward skew must be tolerated")
	}

	// The repository must have recorded the past step, not the current step.
	if repo.lastClaimStep != expectedStep {
		t.Errorf("ClaimTOTPStep called with step=%d, want past step=%d (current=%d); "+
			"a mutant that ignores delta would claim the wrong step",
			repo.lastClaimStep, expectedStep, currentStep)
	}
}

// TestTOTPService_findValidatedTOTPStep_FutureCodeClaimsFutureStep asserts
// that a code from the next step claims the next step in the repository.
// A mutant that sets step := currentStep ignoring delta would claim the
// current step for a future-step code, which is wrong.
func TestTOTPService_findValidatedTOTPStep_FutureCodeClaimsFutureStep(t *testing.T) {
	svc, repo := totpserviceCovNewSvc(t)
	rawSecret, encryptedSecret := totpserviceCovEnroll(t, svc, repo, 21)

	now := time.Now()
	currentStep := now.Unix() / totpStepSeconds
	expectedStep := currentStep + 1

	futureCode, err := totp.GenerateCode(rawSecret, time.Unix(expectedStep*totpStepSeconds, 0))
	if err != nil {
		t.Fatalf("GenerateCode (future step %d): %v", expectedStep, err)
	}

	valid, err := svc.ValidateCode(context.Background(), 21, encryptedSecret, futureCode)
	if err != nil {
		t.Fatalf("ValidateCode with future-step code: %v", err)
	}
	if !valid {
		t.Fatal("ValidateCode rejected a future-step code — forward skew must be tolerated")
	}

	if repo.lastClaimStep != expectedStep {
		t.Errorf("ClaimTOTPStep called with step=%d, want future step=%d (current=%d); "+
			"a mutant that ignores delta would claim the wrong step",
			repo.lastClaimStep, expectedStep, currentStep)
	}
}

// ---------------------------------------------------------------------------
// Line 193 – subtle.ConstantTimeCompare(...)== 1: wrong code must not match
// ---------------------------------------------------------------------------

// TestTOTPService_findValidatedTOTPStep_WrongCodeRejected asserts that a code
// that differs from the valid code by exactly one digit is rejected.
// A mutant that inverts the comparison (== 0 instead of == 1) would accept
// the wrong code, causing this test to fail.
func TestTOTPService_findValidatedTOTPStep_WrongCodeRejected(t *testing.T) {
	svc, repo := totpserviceCovNewSvc(t)
	rawSecret, encryptedSecret := totpserviceCovEnroll(t, svc, repo, 30)

	// Generate the valid code for all three windows (current, ±1 step).
	now := time.Now()
	currentStep := now.Unix() / totpStepSeconds
	validCodes := make(map[string]bool)
	for _, delta := range []int64{-1, 0, +1} {
		c, err := totp.GenerateCode(rawSecret, time.Unix((currentStep+delta)*totpStepSeconds, 0))
		if err == nil {
			validCodes[c] = true
		}
	}

	// Iterate through all 6-digit codes and find one that is not valid for any step.
	wrongCode := ""
	for n := 0; n <= 999999; n++ {
		candidate := fmt.Sprintf("%06d", n)
		if !validCodes[candidate] {
			wrongCode = candidate
			break
		}
	}
	if wrongCode == "" {
		t.Skip("all 6-digit codes happen to be valid (astronomically unlikely)")
	}

	valid, err := svc.ValidateCode(context.Background(), 30, encryptedSecret, wrongCode)
	if err != nil {
		t.Fatalf("ValidateCode with wrong code returned unexpected error: %v", err)
	}
	if valid {
		t.Errorf("ValidateCode accepted wrong code %q — ConstantTimeCompare logic is broken", wrongCode)
	}

	// Replay protection: since no valid code was consumed, the stub's claimedStep
	// for user 30 should not have been advanced.
	if repo.lastClaimStep != 0 {
		t.Errorf("ClaimTOTPStep was unexpectedly called with step=%d for a rejected code", repo.lastClaimStep)
	}
	_ = repo // suppress unused warning
}

// TestTOTPService_findValidatedTOTPStep_ValidCodeAccepted is a direct unit
// test of findValidatedTOTPStep to assert it returns found=true and the
// correct step when given a valid code at the current step. Together with
// WrongCodeRejected above, any mutation of the == 1 comparison is killed.
func TestTOTPService_findValidatedTOTPStep_ValidCodeAccepted(t *testing.T) {
	rawSecret := "JBSWY3DPEHPK3PXP"
	now := time.Now()
	currentStep := now.Unix() / totpStepSeconds

	code, err := totp.GenerateCode(rawSecret, now)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	step, found := findValidatedTOTPStep(rawSecret, code, now)
	if !found {
		t.Fatal("findValidatedTOTPStep returned found=false for a valid current-step code")
	}
	if step != currentStep {
		t.Errorf("findValidatedTOTPStep returned step=%d, want currentStep=%d", step, currentStep)
	}
}

// ---------------------------------------------------------------------------
// Lines 18 & 20 – DefaultTOTPAttemptsWindow / DefaultTOTPDisableAttemptsWindow
// ---------------------------------------------------------------------------

// TestTOTPService_CheckRateLimit_WindowExpiry asserts that a failure recorded
// more than DefaultTOTPAttemptsWindow ago does NOT contribute to the rate
// limit. This directly exercises the window constant on line 18: if the
// constant were mutated to a very small value the failure would fall outside
// the window and the limiter would never trip; if mutated to a very large
// value a stale attempt that should have expired would still count.
func TestTOTPService_CheckRateLimit_WindowExpiry(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte(totpserviceCovSecretKey)
	svc := NewTOTPService(repo, secretKey, nil)

	// Record DefaultTOTPAttemptsLimit failures just outside the window.
	// They must NOT trip the limiter when checked at "now".
	staleTime := time.Now().Add(-(DefaultTOTPAttemptsWindow + time.Second))
	now := time.Now()
	for i := 0; i < DefaultTOTPAttemptsLimit; i++ {
		svc.RecordFailure(secretKey, "10.0.0.1", 50, staleTime)
	}

	if err := svc.CheckRateLimit(secretKey, "10.0.0.1", 50, now); err != nil {
		t.Errorf("CheckRateLimit() = %v after %d failures outside window; want nil (stale attempts must not count)",
			err, DefaultTOTPAttemptsLimit)
	}

	// Now record the same number of failures just inside the window — they must trip the limiter.
	freshTime := now.Add(-(DefaultTOTPAttemptsWindow - time.Second))
	for i := 0; i < DefaultTOTPAttemptsLimit; i++ {
		svc.RecordFailure(secretKey, "10.0.0.2", 51, freshTime)
	}

	if err := svc.CheckRateLimit(secretKey, "10.0.0.2", 51, now); err == nil {
		t.Errorf("CheckRateLimit() = nil after %d failures inside window; want ErrTOTPRateLimited",
			DefaultTOTPAttemptsLimit)
	}
}

// TestTOTPService_CheckDisableRateLimit_WindowExpiry mirrors the above for
// the disable-attempt policy (line 20 DefaultTOTPDisableAttemptsWindow).
func TestTOTPService_CheckDisableRateLimit_WindowExpiry(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte(totpserviceCovSecretKey)
	svc := NewTOTPService(repo, secretKey, nil)

	staleTime := time.Now().Add(-(DefaultTOTPDisableAttemptsWindow + time.Second))
	now := time.Now()
	for i := 0; i < DefaultTOTPDisableAttemptsLimit; i++ {
		svc.RecordDisableFailure(secretKey, "10.0.0.3", 60, staleTime)
	}

	if err := svc.CheckDisableRateLimit(secretKey, "10.0.0.3", 60, now); err != nil {
		t.Errorf("CheckDisableRateLimit() = %v after %d failures outside window; want nil",
			err, DefaultTOTPDisableAttemptsLimit)
	}

	freshTime := now.Add(-(DefaultTOTPDisableAttemptsWindow - time.Second))
	for i := 0; i < DefaultTOTPDisableAttemptsLimit; i++ {
		svc.RecordDisableFailure(secretKey, "10.0.0.4", 61, freshTime)
	}

	if err := svc.CheckDisableRateLimit(secretKey, "10.0.0.4", 61, now); err == nil {
		t.Errorf("CheckDisableRateLimit() = nil after %d failures inside window; want ErrTOTPDisableRateLimited",
			DefaultTOTPDisableAttemptsLimit)
	}
}
