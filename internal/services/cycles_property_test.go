package services

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Property-based tests for the core cycle math. rapid draws thousands of
// (cycle, luteal, date) combinations and shrinks any counterexample to a
// minimal failing case. These pin behavioral invariants that line-coverage
// alone cannot: bounds, calculability thresholds, and the geometry of the
// predicted fertile window.

func cycleDaysBetween(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}

func drawCycleStartDate(t *rapid.T) time.Time {
	year := rapid.IntRange(1971, 2100).Draw(t, "year")
	month := rapid.IntRange(1, 12).Draw(t, "month")
	day := rapid.IntRange(1, 28).Draw(t, "day")
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// TestResolveLutealPhaseProperty pins the clamping contract band-by-band and
// guarantees the result never drops below the supported minimum.
func TestResolveLutealPhaseProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := rapid.IntRange(-500, 500).Draw(t, "luteal")
		got := ResolveLutealPhase(value)

		if got < minLutealPhaseDays {
			t.Fatalf("ResolveLutealPhase(%d)=%d below minimum %d", value, got, minLutealPhaseDays)
		}

		var want int
		switch {
		case value <= 0:
			want = defaultLutealPhaseDays
		case value < minLutealPhaseDays:
			want = minLutealPhaseDays
		default:
			want = value
		}
		if got != want {
			t.Fatalf("ResolveLutealPhase(%d)=%d, want %d", value, got, want)
		}
	})
}

// TestCalcOvulationDayProperty checks that any calculable ovulation day lands
// strictly inside the cycle and respects the luteal-phase reserve.
func TestCalcOvulationDayProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cycleLen := rapid.IntRange(1, 400).Draw(t, "cycleLen")
		luteal := rapid.IntRange(-100, 400).Draw(t, "luteal")

		ovDay, ok := CalcOvulationDay(cycleLen, luteal)
		if !ok {
			return
		}
		if ovDay < minOvulationCycleDay {
			t.Fatalf("ovDay %d below %d (cycle %d, luteal %d)", ovDay, minOvulationCycleDay, cycleLen, luteal)
		}
		if ovDay > cycleLen-minLutealPhaseDays {
			t.Fatalf("ovDay %d exceeds cycleLen-minLuteal=%d (cycle %d)", ovDay, cycleLen-minLutealPhaseDays, cycleLen)
		}
		if ovDay >= cycleLen {
			t.Fatalf("ovDay %d not strictly within cycle %d", ovDay, cycleLen)
		}
	})
}

// TestPredictCycleWindowProperty checks the geometry of the predicted window:
// ovulation falls within the cycle, the fertile window ends at ovulation, never
// starts before the period, and spans at most 5 days.
func TestPredictCycleWindowProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		periodStart := drawCycleStartDate(t)
		cycleLen := rapid.IntRange(1, 120).Draw(t, "cycleLen")
		luteal := rapid.IntRange(-50, 120).Draw(t, "luteal")

		ovulation, fertStart, fertEnd, _, calculable := PredictCycleWindow(periodStart, cycleLen, luteal)
		if !calculable {
			return
		}

		nextPeriod := periodStart.AddDate(0, 0, cycleLen)

		if ovulation.Before(periodStart) {
			t.Fatalf("ovulation %s before periodStart %s", ovulation, periodStart)
		}
		if !ovulation.Before(nextPeriod) {
			t.Fatalf("ovulation %s not before next period %s", ovulation, nextPeriod)
		}
		if !fertEnd.Equal(ovulation) {
			t.Fatalf("fertilityEnd %s != ovulation %s", fertEnd, ovulation)
		}
		if fertStart.After(fertEnd) {
			t.Fatalf("fertilityStart %s after fertilityEnd %s", fertStart, fertEnd)
		}
		if fertStart.Before(periodStart) {
			t.Fatalf("fertilityStart %s before periodStart %s", fertStart, periodStart)
		}
		if span := cycleDaysBetween(fertStart, fertEnd); span < 0 || span > 5 {
			t.Fatalf("fertile window span %d days out of [0,5]", span)
		}
		for _, d := range []time.Time{ovulation, fertStart, fertEnd} {
			if h, m, s := d.Clock(); h != 0 || m != 0 || s != 0 || d.Nanosecond() != 0 {
				t.Fatalf("non-midnight output %s", d)
			}
		}
	})
}
