package services

// totp_service_mutation_round2_test.go
//
// Round-2 mutation-survivor kill tests for totp_service.go. Every assertion is
// against an observable return value (a decision), never a log string or markup.
//
// Helpers/fixtures unique to this file are prefixed "mut2totpservice" to avoid
// symbol collisions with Round-1 (totpserviceCov*) and other round-2 files.

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// TestTOTPService_findValidatedTOTPStep_AdjacentCollisionPrefersPastStep kills
// the ARITHMETIC_BASE mutant at totp_service.go line 189
// (`step := currentStep + delta` -> `step := currentStep - delta`).
//
// The loop iterates delta over []int64{0, -1, +1} and returns on the FIRST
// matching step. Negating the sign does not change the SET of steps probed
// {S-1, S, S+1}; it only reverses the visit ORDER of the two skew windows:
//
//	original: [S, S-1, S+1]  -> first adjacent match is the PAST step  (S-1)
//	mutant:   [S, S+1, S-1]  -> first adjacent match is the FUTURE step (S+1)
//
// Order only matters when the SAME code is valid at two windows. We pin a fixed
// clock and a secret for which code(S-1) == code(S+1) and code(S) differs, then
// feed that colliding code. The original returns S-1; the mutant returns S+1.
//
// The returned step is load-bearing: ValidateCode hands it to ClaimTOTPStep
// (line 159), which advances totp_last_used_step under strictly-greater replay
// semantics. Claiming S+1 instead of S-1 consumes the wrong window and would
// let the genuine S step still be replayed, so this is a real defect, not a
// cosmetic ordering change.
//
// Fixture derived offline by brute-force search against pquerna/otp v1.5.0
// (period = 30s) at the fixed instant below; it is a stable constant tied to a
// fixed injected clock, so it changes only if the TOTP algorithm/period itself
// changes — at which point the guard trips loudly rather than passing silently.
func TestTOTPService_findValidatedTOTPStep_AdjacentCollisionPrefersPastStep(t *testing.T) {
	const mut2totpserviceCollidingSecret = "53QMWFBK3KWC4KFNCMZSP37SHKGEJNEG"

	now := time.Unix(1700000000, 0)
	currentStep := now.Unix() / totpStepSeconds // 56666666

	// Self-documenting fixture guard: the code must collide on (S-1, S+1) and
	// differ from code(S). If the upstream TOTP algorithm ever changes, this
	// trips with a clear message instead of silently no longer discriminating.
	prev, err := totp.GenerateCode(mut2totpserviceCollidingSecret, time.Unix((currentStep-1)*totpStepSeconds, 0))
	if err != nil {
		t.Fatalf("GenerateCode(S-1): %v", err)
	}
	cur, err := totp.GenerateCode(mut2totpserviceCollidingSecret, time.Unix(currentStep*totpStepSeconds, 0))
	if err != nil {
		t.Fatalf("GenerateCode(S): %v", err)
	}
	next, err := totp.GenerateCode(mut2totpserviceCollidingSecret, time.Unix((currentStep+1)*totpStepSeconds, 0))
	if err != nil {
		t.Fatalf("GenerateCode(S+1): %v", err)
	}
	if prev != next || prev == cur {
		t.Fatalf("fixture no longer discriminates: code(S-1)=%s code(S)=%s code(S+1)=%s "+
			"(need S-1==S+1 and != S); re-derive the collision secret", prev, cur, next)
	}

	// `prev` is the code valid at both adjacent windows but not the current one.
	step, found := findValidatedTOTPStep(mut2totpserviceCollidingSecret, prev, now)
	if !found {
		t.Fatal("findValidatedTOTPStep returned found=false for a code valid at S-1 and S+1")
	}
	// Original visits [S, S-1, S+1] and returns the PAST step on first match.
	// The mutant (step := currentStep - delta) visits [S, S+1, S-1] and returns
	// the FUTURE step (currentStep+1) instead.
	if step != currentStep-1 {
		t.Errorf("findValidatedTOTPStep returned step=%d, want past step=%d (current=%d); "+
			"step %d means delta is being subtracted not added (ARITHMETIC_BASE +->-), which "+
			"claims the wrong window for an adjacent-collision code",
			step, currentStep-1, currentStep, currentStep+1)
	}
}
