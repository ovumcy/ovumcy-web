package services

import (
	"testing"
	"time"
)

func TestCalcOvulationDay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		cycleLength int
		lutealPhase int
		wantDay     int
		wantExact   bool
	}{
		{name: "regular cycle", cycleLength: 28, lutealPhase: 14, wantDay: 14, wantExact: true},
		{name: "short cycle keeps minimum luteal reserve", cycleLength: 15, lutealPhase: 10, wantDay: 5, wantExact: true},
		{name: "short cycle clamps high luteal phase to day five", cycleLength: 15, lutealPhase: 15, wantDay: 5, wantExact: false},
		{name: "incompatible when cycle is shorter than the supported prediction floor", cycleLength: 14, lutealPhase: 15, wantDay: 0, wantExact: false},
		{name: "long cycle with personalized luteal", cycleLength: 35, lutealPhase: 12, wantDay: 23, wantExact: true},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			gotDay, gotExact := CalcOvulationDay(testCase.cycleLength, testCase.lutealPhase)
			if gotDay != testCase.wantDay {
				t.Fatalf("expected ovulation day %d, got %d", testCase.wantDay, gotDay)
			}
			if gotExact != testCase.wantExact {
				t.Fatalf("expected exact=%v, got %v", testCase.wantExact, gotExact)
			}
		})
	}
}

func TestPredictCycleWindow_InvalidWhenCycleIsShorterThanSupportedPredictionFloor(t *testing.T) {
	t.Parallel()

	periodStart := mustParseDay(t, "2026-02-10")
	ovulationDate, fertilityStart, fertilityEnd, exact, calculable := PredictCycleWindow(periodStart, 14, 15)

	if calculable {
		t.Fatalf("expected incompatible values to be non-calculable")
	}
	if exact {
		t.Fatalf("expected exact=false for non-calculable prediction")
	}
	if !ovulationDate.IsZero() || !fertilityStart.IsZero() || !fertilityEnd.IsZero() {
		t.Fatalf("expected zero dates for incompatible values, got ov=%s fs=%s fe=%s",
			ovulationDate.Format("2006-01-02"),
			fertilityStart.Format("2006-01-02"),
			fertilityEnd.Format("2006-01-02"))
	}
}

func TestPredictCycleWindow_FertilityWindowMayOverlapPeriodDays(t *testing.T) {
	t.Parallel()

	periodStart := mustParseDay(t, "2026-02-10")
	ovulationDate, fertilityStart, fertilityEnd, exact, calculable := PredictCycleWindow(periodStart, 15, 10)

	if !calculable {
		t.Fatalf("expected calculable prediction for short cycle")
	}
	if !exact {
		t.Fatalf("expected exact=true for calculable prediction")
	}
	if got := ovulationDate.Format("2006-01-02"); got != "2026-02-14" {
		t.Fatalf("expected ovulation date 2026-02-14, got %s", got)
	}
	if got := fertilityStart.Format("2006-01-02"); got != "2026-02-10" {
		t.Fatalf("expected fertility start 2026-02-10, got %s", got)
	}
	if got := fertilityEnd.Format("2006-01-02"); got != "2026-02-14" {
		t.Fatalf("expected fertility end 2026-02-14, got %s", got)
	}

	assertCyclePredictionInvariants(t, periodStart, 15, ovulationDate, fertilityStart, fertilityEnd)
}

func TestPredictCycleWindow_NormalCycle(t *testing.T) {
	t.Parallel()

	periodStart := mustParseDay(t, "2026-02-10")
	ovulationDate, fertilityStart, fertilityEnd, exact, calculable := PredictCycleWindow(periodStart, 28, 14)

	if !calculable {
		t.Fatalf("expected calculable prediction for regular cycle")
	}
	if !exact {
		t.Fatalf("expected exact prediction for regular cycle")
	}
	if got := ovulationDate.Format("2006-01-02"); got != "2026-02-23" {
		t.Fatalf("expected ovulation date 2026-02-23, got %s", got)
	}
	if got := fertilityStart.Format("2006-01-02"); got != "2026-02-18" {
		t.Fatalf("expected fertility start 2026-02-18, got %s", got)
	}
	if got := fertilityEnd.Format("2006-01-02"); got != "2026-02-23" {
		t.Fatalf("expected fertility end 2026-02-23, got %s", got)
	}

	assertCyclePredictionInvariants(t, periodStart, 28, ovulationDate, fertilityStart, fertilityEnd)
}

func TestPredictCycleWindow_UsesOneBasedCycleDayCounting(t *testing.T) {
	t.Parallel()

	periodStart := mustParseDay(t, "2026-03-10")
	ovulationDate, _, _, exact, calculable := PredictCycleWindow(periodStart, 28, 14)

	if !calculable {
		t.Fatalf("expected calculable prediction for regular cycle")
	}
	if !exact {
		t.Fatalf("expected exact prediction for regular cycle")
	}
	if got := ovulationDate.Format("2006-01-02"); got != "2026-03-23" {
		t.Fatalf("expected one-based cycle day 14 to map to 2026-03-23, got %s", got)
	}
}

func TestPredictCycleWindow_InvariantsAcrossRanges(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		cycleLength    int
		lutealPhase    int
		wantCalculable bool
	}{
		{name: "below supported floor", cycleLength: 14, lutealPhase: 15, wantCalculable: false},
		{name: "short cycle with clamped luteal", cycleLength: 15, lutealPhase: 15, wantCalculable: true},
		{name: "short cycle", cycleLength: 15, lutealPhase: 10, wantCalculable: true},
		{name: "regular", cycleLength: 28, lutealPhase: 14, wantCalculable: true},
		{name: "personalized", cycleLength: 35, lutealPhase: 12, wantCalculable: true},
		{name: "max cycle", cycleLength: 90, lutealPhase: 14, wantCalculable: true},
	}

	periodStart := mustParseDay(t, "2026-02-10")
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			ovulationDate, fertilityStart, fertilityEnd, _, calculable := PredictCycleWindow(
				periodStart,
				testCase.cycleLength,
				testCase.lutealPhase,
			)
			if calculable != testCase.wantCalculable {
				t.Fatalf("expected calculable=%v, got %v", testCase.wantCalculable, calculable)
			}
			if !calculable {
				if !ovulationDate.IsZero() || !fertilityStart.IsZero() || !fertilityEnd.IsZero() {
					t.Fatalf("expected zero dates for non-calculable prediction")
				}
				return
			}
			assertCyclePredictionInvariants(t, periodStart, testCase.cycleLength, ovulationDate, fertilityStart, fertilityEnd)
		})
	}
}

func assertCyclePredictionInvariants(t *testing.T, periodStart time.Time, cycleLength int, ovulationDate time.Time, fertilityStart time.Time, fertilityEnd time.Time) {
	t.Helper()

	nextPeriodStart := periodStart.AddDate(0, 0, cycleLength)

	if ovulationDate.IsZero() {
		t.Fatalf("ovulation date must not be zero")
	}
	if !ovulationDate.Before(nextPeriodStart) {
		t.Fatalf("ovulation %s must be before next period start %s", ovulationDate.Format("2006-01-02"), nextPeriodStart.Format("2006-01-02"))
	}

	if fertilityStart.IsZero() || fertilityEnd.IsZero() {
		return
	}
	if fertilityStart.Before(periodStart) {
		t.Fatalf("fertility start %s must not be before cycle start %s", fertilityStart.Format("2006-01-02"), periodStart.Format("2006-01-02"))
	}
	if fertilityStart.After(fertilityEnd) {
		t.Fatalf("fertility start %s must not be after fertility end %s", fertilityStart.Format("2006-01-02"), fertilityEnd.Format("2006-01-02"))
	}
	if !fertilityEnd.Before(nextPeriodStart) {
		t.Fatalf("fertility end %s must be before next period start %s", fertilityEnd.Format("2006-01-02"), nextPeriodStart.Format("2006-01-02"))
	}
}
