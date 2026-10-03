package services

import "testing"

// Every expected date below is counted by hand on the printed calendar
// (February has 29 days in 2000 and 2024, 28 in 2025 and 2100), never derived
// from the implementation. Ovulation is cycle day (cycleLength - luteal), one-based,
// so it falls (ovulationDay - 1) days after the period start; the fertile window is
// the six days ending on it, clamped to the period start.
func TestPredictCycleWindow_AcrossFebruary29(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		periodStart string
		cycleLength int
		lutealPhase int
		wantOvul    string
		wantFertile string
		wantFertEnd string
	}{
		{name: "leap year ovulation lands on Feb 29", periodStart: "2024-02-16", cycleLength: 28, lutealPhase: 14, wantOvul: "2024-02-29", wantFertile: "2024-02-24", wantFertEnd: "2024-02-29"},
		{name: "non-leap year same start rolls to Mar 1", periodStart: "2025-02-16", cycleLength: 28, lutealPhase: 14, wantOvul: "2025-03-01", wantFertile: "2025-02-24", wantFertEnd: "2025-03-01"},
		{name: "leap year window ends on Feb 28", periodStart: "2024-02-15", cycleLength: 28, lutealPhase: 14, wantOvul: "2024-02-28", wantFertile: "2024-02-23", wantFertEnd: "2024-02-28"},
		{name: "non-leap year window ends on Feb 28 a day later start", periodStart: "2025-02-15", cycleLength: 28, lutealPhase: 14, wantOvul: "2025-02-28", wantFertile: "2025-02-23", wantFertEnd: "2025-02-28"},
		{name: "leap year window ends on Mar 1", periodStart: "2024-02-17", cycleLength: 28, lutealPhase: 14, wantOvul: "2024-03-01", wantFertile: "2024-02-25", wantFertEnd: "2024-03-01"},
		{name: "leap year fertile window spans the leap day", periodStart: "2024-02-20", cycleLength: 28, lutealPhase: 14, wantOvul: "2024-03-04", wantFertile: "2024-02-28", wantFertEnd: "2024-03-04"},
		{name: "non-leap year contrast of the spanning window", periodStart: "2025-02-20", cycleLength: 28, lutealPhase: 14, wantOvul: "2025-03-05", wantFertile: "2025-02-28", wantFertEnd: "2025-03-05"},
		{name: "period start before leap day, window after it", periodStart: "2024-02-10", cycleLength: 40, lutealPhase: 14, wantOvul: "2024-03-06", wantFertile: "2024-03-01", wantFertEnd: "2024-03-06"},
		{name: "same long cycle in a non-leap year", periodStart: "2025-02-10", cycleLength: 40, lutealPhase: 14, wantOvul: "2025-03-07", wantFertile: "2025-03-02", wantFertEnd: "2025-03-07"},
		{name: "period start in January, window spans the leap day", periodStart: "2024-01-31", cycleLength: 45, lutealPhase: 14, wantOvul: "2024-03-01", wantFertile: "2024-02-25", wantFertEnd: "2024-03-01"},
		{name: "same January start in a non-leap year", periodStart: "2025-01-31", cycleLength: 45, lutealPhase: 14, wantOvul: "2025-03-02", wantFertile: "2025-02-25", wantFertEnd: "2025-03-02"},
		{name: "period start on the leap day", periodStart: "2024-02-29", cycleLength: 28, lutealPhase: 14, wantOvul: "2024-03-13", wantFertile: "2024-03-08", wantFertEnd: "2024-03-13"},
		{name: "clamp to period start across the leap day", periodStart: "2024-02-28", cycleLength: 15, lutealPhase: 10, wantOvul: "2024-03-03", wantFertile: "2024-02-28", wantFertEnd: "2024-03-03"},
		{name: "clamp to period start in a non-leap year", periodStart: "2025-02-28", cycleLength: 15, lutealPhase: 10, wantOvul: "2025-03-04", wantFertile: "2025-02-28", wantFertEnd: "2025-03-04"},
		{name: "century leap year 2000 has Feb 29", periodStart: "2000-02-16", cycleLength: 28, lutealPhase: 14, wantOvul: "2000-02-29", wantFertile: "2000-02-24", wantFertEnd: "2000-02-29"},
		{name: "century non-leap year 2100 has no Feb 29", periodStart: "2100-02-16", cycleLength: 28, lutealPhase: 14, wantOvul: "2100-03-01", wantFertile: "2100-02-24", wantFertEnd: "2100-03-01"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			window := PredictCycleWindow(mustParseDay(t, testCase.periodStart), testCase.cycleLength, testCase.lutealPhase)
			if !window.Calculable || !window.OvulationExact {
				t.Fatalf("expected an exact calculable window, got calculable=%v exact=%v", window.Calculable, window.OvulationExact)
			}
			if got := window.OvulationDate.Format("2006-01-02"); got != testCase.wantOvul {
				t.Fatalf("ovulation: expected %s, got %s", testCase.wantOvul, got)
			}
			if got := window.FertilityWindowStart.Format("2006-01-02"); got != testCase.wantFertile {
				t.Fatalf("fertility start: expected %s, got %s", testCase.wantFertile, got)
			}
			if got := window.FertilityWindowEnd.Format("2006-01-02"); got != testCase.wantFertEnd {
				t.Fatalf("fertility end: expected %s, got %s", testCase.wantFertEnd, got)
			}
		})
	}
}
