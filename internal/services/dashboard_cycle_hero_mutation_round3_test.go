package services

import "testing"

// dashboard_cycle_hero.go: dashboardCycleHeroCurrentPhase boundary mutants.
// Called directly with an empty phase string so the day-based switch runs.
// Signature: (currentPhase, currentDay, periodLength, ovulationDay, cycleLength).
func TestMR3Dash_CycleHeroCurrentPhaseDayBoundaries(t *testing.T) {
	const (
		periodLength = 5
		ovulationDay = 14
		cycleLength  = 28
	)

	cases := []struct {
		name string
		day  int
		want string
	}{
		// day==1 -> menstrual kills 154:18 (`>=1`->`>1`)
		{"period_start_day1", 1, "menstrual"},
		// day==periodLength -> menstrual kills 154:37 (`<=`->`<`)
		{"period_end_dayPeriodLength", periodLength, "menstrual"},
		// day==ovulationDay+1 -> luteal kills 158:18 (`>`)
		{"luteal_start_dayOvulationPlus1", ovulationDay + 1, "luteal"},
		// day==cycleLength -> luteal kills 158:47 (`<=`->`<`)
		{"luteal_end_dayCycleLength", cycleLength, "luteal"},
		// day==ovulationDay -> ovulation pins 156 (`==`->`!=`)
		{"ovulation_dayOvulation", ovulationDay, "ovulation"},
		// day==6 (between period end and ovulation) -> follicular default branch
		{"follicular_day6", 6, "follicular"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dashboardCycleHeroCurrentPhase("", tc.day, periodLength, ovulationDay, cycleLength)
			if got != tc.want {
				t.Fatalf("dashboardCycleHeroCurrentPhase(\"\", %d, %d, %d, %d) = %q, want %q",
					tc.day, periodLength, ovulationDay, cycleLength, got, tc.want)
			}
		})
	}
}
