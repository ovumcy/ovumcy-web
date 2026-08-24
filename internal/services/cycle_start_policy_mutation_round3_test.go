package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestMR3Cycles_PotentialImplantationUsesTheDefaultCycleLengthWithoutLogs pins
// implantation detection for the no-observed-data path: with no daily logs
// predictedCycleLength falls through to models.DefaultCycleLength (28) and the
// detection runs on that default. It does NOT cover the
// cycleLength <= 0 fallback at cycle_start_policy.go:83-88 — nothing this
// caller can build reaches it (the reason is recorded there), which is why the
// `<= 0` -> `< 0` mutant on those two lines is equivalent and why the name no
// longer advertises a zero-cycle-length fallback.
func TestMR3Cycles_PotentialImplantationUsesTheDefaultCycleLengthWithoutLogs(t *testing.T) {
	location := time.UTC
	// Owner with a configured 28-day cycle and an explicit last period start,
	// but NO daily logs -> no observed cycle lengths -> predictedCycleLength
	// returns its models.DefaultCycleLength (28) fallback.
	lastPeriod := mr3cycDay(2026, time.March, 1)
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     28,
		PeriodLength:    5,
		LutealPhase:     14,
		LastPeriodStart: &lastPeriod,
	}

	// Ovulation for a 28-day cycle anchored Mar 1 falls on cycle day 14 ->
	// Mar 14. An implantation candidate sits 6-12 days after ovulation; pick a
	// target day 8 days after ovulation (Mar 22).
	target := mr3cycDay(2026, time.March, 22)
	now := mr3cycDay(2026, time.March, 22)

	policy := ResolveManualCycleStartPolicy(user, nil, target, now, location)
	if !policy.PotentialImplantation {
		t.Fatalf("expected implantation detection on the default 28-day cycle, got none (gap=%d)",
			policy.ImplantationGapDays)
	}
}
