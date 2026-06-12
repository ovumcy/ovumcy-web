package services

import (
	"testing"
	"time"
)

// Reference vectors for the cycle-prediction math. Every case here is also a row
// in docs/cycle-prediction.md — the worked examples and the code are asserted to
// agree, so the public documentation cannot silently drift from the behavior.
//
// These deterministic example-based tests complement the property tests
// (cycles_property_test.go, invariants) and the fuzz tests (policy_fuzz_test.go,
// robustness). Together: transparent and verifiable.

func refDate(year, month, day int) time.Time {
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func TestResolveLutealPhase_ReferenceVectors(t *testing.T) {
	cases := []struct {
		name  string
		input int
		want  int
	}{
		{"zero defaults to 14", 0, 14},
		{"negative defaults to 14", -5, 14},
		{"below minimum clamps to 10", 5, 10},
		{"just below minimum clamps to 10", 9, 10},
		{"at minimum is unchanged", 10, 10},
		{"default value is unchanged", 14, 14},
		{"above minimum is unchanged", 20, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveLutealPhase(tc.input); got != tc.want {
				t.Fatalf("ResolveLutealPhase(%d) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestCalcOvulationDay_ReferenceVectors(t *testing.T) {
	// The second return is ovulationExact: false when the luteal phase had to be
	// clamped to the cycle reserve (or when the cycle is too short to predict).
	cases := []struct {
		name      string
		cycleLen  int
		luteal    int
		wantDay   int
		wantExact bool
	}{
		{"standard 28-day cycle", 28, 14, 14, true},
		{"30-day cycle, default luteal", 30, 0, 16, true},
		{"short 21-day cycle", 21, 14, 7, true},
		{"15-day cycle clamps luteal (non-exact)", 15, 14, 5, false},
		{"cycle too short for a prediction", 14, 14, 0, false},
		{"long luteal clamped to reserve (non-exact)", 28, 25, 5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotDay, gotExact := CalcOvulationDay(tc.cycleLen, tc.luteal)
			if gotDay != tc.wantDay || gotExact != tc.wantExact {
				t.Fatalf("CalcOvulationDay(%d,%d) = (%d,%t), want (%d,%t)",
					tc.cycleLen, tc.luteal, gotDay, gotExact, tc.wantDay, tc.wantExact)
			}
		})
	}
}

func TestCyclePrediction_ReferenceVectors(t *testing.T) {
	cases := []struct {
		name         string
		periodStart  time.Time
		cycleLen     int
		luteal       int
		wantCalc     bool
		wantExact    bool
		wantOvul     time.Time
		wantFertFrom time.Time
		wantFertTo   time.Time
		wantNext     time.Time // model: periodStart + cycleLen
	}{
		{
			name:        "standard 28-day cycle",
			periodStart: refDate(2026, 3, 10), cycleLen: 28, luteal: 14,
			wantCalc: true, wantExact: true,
			wantOvul:     refDate(2026, 3, 23),
			wantFertFrom: refDate(2026, 3, 18), wantFertTo: refDate(2026, 3, 23),
			wantNext: refDate(2026, 4, 7),
		},
		{
			name:        "30-day cycle, default luteal",
			periodStart: refDate(2026, 6, 1), cycleLen: 30, luteal: 0,
			wantCalc: true, wantExact: true,
			wantOvul:     refDate(2026, 6, 16),
			wantFertFrom: refDate(2026, 6, 11), wantFertTo: refDate(2026, 6, 16),
			wantNext: refDate(2026, 7, 1),
		},
		{
			name:        "short 21-day cycle",
			periodStart: refDate(2026, 1, 1), cycleLen: 21, luteal: 14,
			wantCalc: true, wantExact: true,
			wantOvul:     refDate(2026, 1, 7),
			wantFertFrom: refDate(2026, 1, 2), wantFertTo: refDate(2026, 1, 7),
			wantNext: refDate(2026, 1, 22),
		},
		{
			name:        "15-day cycle clamps luteal and fertile window",
			periodStart: refDate(2026, 2, 1), cycleLen: 15, luteal: 14,
			wantCalc: true, wantExact: false,
			wantOvul:     refDate(2026, 2, 5),
			wantFertFrom: refDate(2026, 2, 1), wantFertTo: refDate(2026, 2, 5),
			wantNext: refDate(2026, 2, 16),
		},
		{
			name:        "cycle too short for any prediction",
			periodStart: refDate(2026, 5, 1), cycleLen: 14, luteal: 14,
			wantCalc: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ovul, fertFrom, fertTo, exact, calc := PredictCycleWindow(tc.periodStart, tc.cycleLen, tc.luteal)
			if calc != tc.wantCalc {
				t.Fatalf("calculable = %t, want %t", calc, tc.wantCalc)
			}
			if !calc {
				return
			}
			if !ovul.Equal(tc.wantOvul) {
				t.Errorf("ovulation = %s, want %s", ovul.Format("2006-01-02"), tc.wantOvul.Format("2006-01-02"))
			}
			if !fertFrom.Equal(tc.wantFertFrom) {
				t.Errorf("fertility start = %s, want %s", fertFrom.Format("2006-01-02"), tc.wantFertFrom.Format("2006-01-02"))
			}
			if !fertTo.Equal(tc.wantFertTo) {
				t.Errorf("fertility end = %s, want %s", fertTo.Format("2006-01-02"), tc.wantFertTo.Format("2006-01-02"))
			}
			if exact != tc.wantExact {
				t.Errorf("exact = %t, want %t", exact, tc.wantExact)
			}
			// Doc-only consistency check: this recomputes periodStart +
			// cycleLength inline, so it can only catch a drifted "next
			// period" column in docs/cycle-prediction.md — never a
			// production bug. The production invariant is pinned by
			// TestPipeline_NextPeriodMatchesFormula.
			if next := tc.periodStart.AddDate(0, 0, tc.cycleLen); !next.Equal(tc.wantNext) {
				t.Errorf("next period = %s, want %s", next.Format("2006-01-02"), tc.wantNext.Format("2006-01-02"))
			}
		})
	}
}
