package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestMR3Cycles_PotentialImplantationRefusesTheDefaultCycleLength pins the
// first-cycle floor at the end of the policy, on the one history that reaches
// the hint with nothing recorded behind it: the anchor comes from the settings
// LastPeriodStart, not from a log, so there are no observed cycle lengths and
// predictedCycleLength would answer with models.DefaultCycleLength. The hint
// counted from that default is an inference presented as a measurement, so it
// is withheld — the FertilityProjectionSuppressed -> PredictionsSuppressed
// mutant reddens here.
//
// It does NOT cover the cycleLength <= 0 fallback further down — nothing this
// caller can build reaches it (the reason is recorded there), which is why the
// `<= 0` -> `< 0` mutant on those two lines is equivalent and why the name
// never advertised a zero-cycle-length fallback.
func TestMR3Cycles_PotentialImplantationRefusesTheDefaultCycleLength(t *testing.T) {
	location := time.UTC
	// Owner with a configured 28-day cycle and an explicit last period start,
	// but NO daily logs -> no recorded cycle starts, no observed cycle lengths.
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
	if policy.PotentialImplantation {
		t.Fatalf("the configuration default is the only source of this ovulation, yet the hint counts %d days from it",
			policy.ImplantationGapDays)
	}

	// Positive anchor: the same day, the same geometry, two recorded 28-day
	// cycles behind it. Without this the assertion above would pass on a hint
	// that had simply stopped working.
	recorded := observedCyclesBefore(lastPeriod)
	if policy := ResolveManualCycleStartPolicy(user, recorded, target, now, location); !policy.PotentialImplantation {
		t.Fatal("scenario setup: an observed 28-day history offers no hint on cycle day 22, so the refusal above proves nothing")
	}
}
