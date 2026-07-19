package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestMR3Cycles_PotentialImplantationZeroCycleLengthFallback pins
// implantation detection for the no-observed-data path. NOTE on
// cycle_start_policy.go:83 BOUNDARY `if cycleLength <= 0` (-> `< 0`): that
// mutant is EQUIVALENT because the guard is effectively dead.
// predictedCycleLength never returns <= 0 — its final fallback returns
// models.DefaultCycleLength (28) — so cycleLength is always positive here and
// the DashboardCycleReferenceLength branch is unreachable in practice. The
// `<= 0` vs `< 0` distinction therefore has no observable effect. This test
// still pins the real behavior: implantation detection fires on the no-data
// path (using the default 28-day cycle that predictedCycleLength supplies).
func TestMR3Cycles_PotentialImplantationZeroCycleLengthFallback(t *testing.T) {
	location := time.UTC
	// Owner with a configured 28-day cycle and an explicit last period start,
	// but NO daily logs -> no observed cycle lengths -> predictedCycleLength==0.
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
		t.Fatalf("expected implantation detection via cycle-length fallback, got none (gap=%d)",
			policy.ImplantationGapDays)
	}
}
