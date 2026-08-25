package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestCycleLengthsCountsCalendarDaysAcrossADSTTransition pins the arithmetic at
// cycleLengths, the one day-difference site of the three in cycles.go whose
// operands do NOT pass through dateOnly inside the function: `starts` arrives as
// a parameter, so the anchor is the caller's business.
//
// Europe/Berlin springs forward on 2026-03-29, so local midnight on 2026-03-28
// and local midnight on 2026-03-30 are 47 hours apart, not 48. Measured in
// hours, `int(47/24)` reports a 1-day cycle; measured in calendar days it is 2.
// The same shortfall shows up with no transition involved whenever one operand
// is a location midnight and the other a UTC one.
func TestCycleLengthsCountsCalendarDaysAcrossADSTTransition(t *testing.T) {
	t.Parallel()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("load Europe/Berlin: %v", err)
	}

	starts := []time.Time{
		time.Date(2026, time.March, 28, 0, 0, 0, 0, berlin),
		time.Date(2026, time.March, 30, 0, 0, 0, 0, berlin),
	}
	if hours := starts[1].Sub(starts[0]).Hours(); hours != 47 {
		t.Fatalf("fixture does not cross the spring-forward day: %.2f hours between the two starts, want 47", hours)
	}

	lengths := cycleLengths(starts)
	if len(lengths) != 1 {
		t.Fatalf("cycleLengths returned %d length(s), want 1", len(lengths))
	}
	if lengths[0] != 2 {
		t.Errorf("cycleLengths across the spring-forward day = %d, want 2 calendar days — the hour difference is 47, which truncates to 1", lengths[0])
	}
}

// TestDetectCycleStartsGapIsACalendarDayGap pins the gap arithmetic in
// DetectCycleStarts. Both operands pass through dateOnly first, so this is a
// characterization: it holds before and after the switch to CalendarDaysBetween
// and exists so the replacement cannot quietly change the off-by-one (`- 1`,
// the count of days BETWEEN two period days) while it changes the instrument.
func TestDetectCycleStartsGapIsACalendarDayGap(t *testing.T) {
	t.Parallel()

	day := func(month time.Month, dayOfMonth int) time.Time {
		return time.Date(2026, month, dayOfMonth, 0, 0, 0, 0, time.UTC)
	}

	cases := []struct {
		name       string
		secondDay  time.Time
		wantStarts int
	}{
		// gapDays = 4: below the >= 5 threshold, so the second period day
		// belongs to the same cycle.
		{name: "four clear days between period days", secondDay: day(time.March, 6), wantStarts: 1},
		// gapDays = 5: exactly the threshold, so a new cycle starts.
		{name: "five clear days between period days", secondDay: day(time.March, 7), wantStarts: 2},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			logs := periodLogsOn(day(time.March, 1), testCase.secondDay)
			if got := len(DetectCycleStarts(logs)); got != testCase.wantStarts {
				t.Errorf("DetectCycleStarts found %d start(s), want %d", got, testCase.wantStarts)
			}
		})
	}
}

// TestBuildPeriodClustersGapIsACalendarDayGap is the same characterization for
// the second gap site, inside buildPeriodClusters. Reached through
// BuildCycleStats, whose PeriodLength counts the days of the first cluster.
func TestBuildPeriodClustersGapIsACalendarDayGap(t *testing.T) {
	t.Parallel()

	day := func(month time.Month, dayOfMonth int) time.Time {
		return time.Date(2026, month, dayOfMonth, 0, 0, 0, 0, time.UTC)
	}

	cases := []struct {
		name         string
		secondDay    time.Time
		wantClusters int
	}{
		{name: "four clear days between period days", secondDay: day(time.March, 6), wantClusters: 1},
		{name: "five clear days between period days", secondDay: day(time.March, 7), wantClusters: 2},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			logs := periodLogsOn(day(time.March, 1), testCase.secondDay)
			if got := len(buildPeriodClusters(logs)); got != testCase.wantClusters {
				t.Errorf("buildPeriodClusters found %d cluster(s), want %d", got, testCase.wantClusters)
			}
		})
	}
}

// periodLogsOn builds one period-flagged log per supplied calendar day.
func periodLogsOn(days ...time.Time) []models.DailyLog {
	logs := make([]models.DailyLog, 0, len(days))
	for _, day := range days {
		logs = append(logs, models.DailyLog{Date: day, IsPeriod: true})
	}
	return logs
}

// eggWhiteLutealLogs builds four 30-day cycles whose only ovulation signal is
// egg-white mucus, placed so every cycle's inferred luteal phase is exactly
// lutealLength days. Ovulation is estimated at peak day + 1, so the peak sits
// lutealLength+1 days before the next cycle start. No BBT values are recorded,
// which keeps the thermal detector out of the inference.
func eggWhiteLutealLogs(lutealLength int) []models.DailyLog {
	const cycleLength = 30
	const cycleCount = 4

	origin := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)
	logs := make([]models.DailyLog, 0, cycleCount*3)
	for cycle := 0; cycle < cycleCount; cycle++ {
		start := origin.AddDate(0, 0, cycle*cycleLength)
		logs = append(logs,
			models.DailyLog{Date: start, IsPeriod: true, CycleStart: true},
			models.DailyLog{Date: start.AddDate(0, 0, 1), IsPeriod: true},
		)
		if cycle == cycleCount-1 {
			continue
		}
		nextStart := start.AddDate(0, 0, cycleLength)
		peak := nextStart.AddDate(0, 0, -(lutealLength + 1))
		logs = append(logs, models.DailyLog{Date: peak, CervicalMucus: models.CervicalMucusEggWhite})
	}
	return logs
}

// TestAppendPredictedCyclesTerminatesOnANonPositiveStep is the termination
// guard for the projection loop in appendPredictedCycles, which advances by
// `cycleStart.AddDate(0, 0, predictedCycleLength)`. A zero step never moves the
// cursor, so the loop bound stays satisfied and the calendar page fills its
// prediction maps until the process dies.
//
// Zero is a real return of predictedCycleLength, not a hypothetical: the average
// branch tests the raw average and returns the rounded one, so 0.4 yields 0.
// Its other stepping caller, applyProjectedBaseline (cycle_baseline.go), reads
// that as "no usable length" and declines to project — this loop had no such
// guard and simply trusted the callee.
//
// Production cannot reach the state (both writers of NextPeriodStart derive
// median and average from the same observed lengths), so the stats below are
// crafted. The call runs in a goroutine behind a deadline because the failure
// mode being guarded is non-termination, which no assertion can observe from
// inside the call.
func TestAppendPredictedCyclesTerminatesOnANonPositiveStep(t *testing.T) {
	t.Parallel()

	if step := predictedCycleLength(0, 0.4); step > 0 {
		t.Fatalf("fixture no longer produces a non-positive step: predictedCycleLength(0, 0.4) = %d — this test would pass without exercising the guard", step)
	}

	stats := CycleStats{
		MedianCycleLength:  0,
		AverageCycleLength: 0.4,
		NextPeriodStart:    time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
	}
	gridEnd := time.Date(2026, time.April, 11, 0, 0, 0, 0, time.UTC)

	predictedPeriod := map[string]bool{}
	preFertile := map[string]bool{}
	fertilityEdge := map[string]bool{}
	fertilityPeak := map[string]bool{}
	ovulation := map[string]bool{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		appendPredictedCycles(predictedPeriod, preFertile, fertilityEdge, fertilityPeak, ovulation, stats, gridEnd, time.UTC, true)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("appendPredictedCycles did not return within 10s on a zero cycle-length step: the projection loop advances by predictedCycleLength and a non-positive step never moves its cursor")
	}

	for name, painted := range map[string]map[string]bool{
		"predicted period": predictedPeriod,
		"pre-fertile":      preFertile,
		"fertility edge":   fertilityEdge,
		"fertility peak":   fertilityPeak,
		"ovulation":        ovulation,
	} {
		if len(painted) != 0 {
			t.Errorf("%s map holds %d day(s); a cycle length that resolves to nothing must paint no projected cycle at all", name, len(painted))
		}
	}
}

// TestCalcOvulationDayAlwaysProducesADayOnceAdmitted proves the claim the two
// `codecov:ignore -- defensive invariant` comments in CalcOvulationDay make.
// Both mark a `return 0, false` that the arithmetic above them makes
// unreachable, and a comment asserting unreachability is worth no more than the
// run behind it: this walks the whole admitted domain and refuses a zero day.
//
// The entry guard admits cycleLen >= minLutealPhaseDays+minOvulationCycleDay,
// so maxSupportedLutealPhase = cycleLen-minOvulationCycleDay is always at least
// minLutealPhaseDays (the first ignored return), and the clamp below it caps
// resolvedLutealPhase at that value, so ovDay is always at least
// minOvulationCycleDay (the second). Deleting the clamp turns the second into a
// live refusal for any luteal phase longer than the cycle supports.
func TestCalcOvulationDayAlwaysProducesADayOnceAdmitted(t *testing.T) {
	t.Parallel()

	const lowestAdmittedCycleLength = minLutealPhaseDays + minOvulationCycleDay

	for cycleLen := lowestAdmittedCycleLength; cycleLen <= 400; cycleLen++ {
		for lutealPhase := -5; lutealPhase <= 60; lutealPhase++ {
			ovDay, _ := CalcOvulationDay(cycleLen, lutealPhase)
			if ovDay < minOvulationCycleDay {
				t.Fatalf("CalcOvulationDay(%d, %d) = %d: a cycle the entry guard already admitted was refused by one of the two defensive returns marked codecov:ignore, so the invariant those comments state does not hold", cycleLen, lutealPhase, ovDay)
			}
			if ovDay >= cycleLen {
				t.Fatalf("CalcOvulationDay(%d, %d) = %d, which is not inside the cycle", cycleLen, lutealPhase, ovDay)
			}
		}
	}
}

// TestBuildStatsCycleFactorRecentCyclesLeavesItsArgumentUntouched pins the
// argument as read-only. Both builders in buildStatsCycleFactorExplanation are
// handed the SAME slice inside one composite literal, so a sort in place makes
// the order of two struct fields load-bearing: reordering them — an edit that
// reads as pure formatting — would hand the pattern builder a different
// sequence. Nothing at either call site says so.
func TestBuildStatsCycleFactorRecentCyclesLeavesItsArgumentUntouched(t *testing.T) {
	t.Parallel()

	day := func(dayOfMonth int) time.Time {
		return time.Date(2026, time.March, dayOfMonth, 0, 0, 0, 0, time.UTC)
	}

	snapshots := []statsCycleFactorCycleSnapshot{
		{Start: day(1), End: day(28), CycleLength: 28, ComparisonKind: "variable"},
		{Start: day(29), End: day(31), CycleLength: 21, ComparisonKind: "shorter"},
		{Start: day(10), End: day(20), CycleLength: 40, ComparisonKind: "longer"},
	}
	before := make([]time.Time, len(snapshots))
	for index, snapshot := range snapshots {
		before[index] = snapshot.Start
	}

	if summaries := buildStatsCycleFactorRecentCycles(snapshots); len(summaries) != len(snapshots) {
		t.Fatalf("buildStatsCycleFactorRecentCycles returned %d summar(y/ies), want %d", len(summaries), len(snapshots))
	}

	for index, snapshot := range snapshots {
		if !snapshot.Start.Equal(before[index]) {
			t.Fatalf("buildStatsCycleFactorRecentCycles reordered its argument: element %d starts on %s, was %s. Sort a copy — the caller passes this same slice to buildStatsCycleFactorPatternSummaries in the same composite literal.", index, snapshot.Start.Format("2006-01-02"), before[index].Format("2006-01-02"))
		}
	}
}

// TestInferUserLutealPhaseRejectsAnImplausiblyLongLutealPhase pins both ends of
// the plausibility window the observed-luteal inference filters on. The floor
// is minLutealPhaseDays and the ceiling maxPlausibleLutealPhaseDays; a cycle
// whose inferred luteal length falls outside either is dropped from the sample,
// so with too few surviving cycles the inference reports the default and
// declines to claim it was refined.
func TestInferUserLutealPhaseRejectsAnImplausiblyLongLutealPhase(t *testing.T) {
	t.Parallel()

	if minLutealPhaseDays >= maxPlausibleLutealPhaseDays {
		t.Fatalf("the plausible-luteal window is empty: floor %d, ceiling %d", minLutealPhaseDays, maxPlausibleLutealPhaseDays)
	}

	cases := []struct {
		name         string
		lutealLength int
		wantRefined  bool
	}{
		{name: "at the ceiling", lutealLength: maxPlausibleLutealPhaseDays, wantRefined: true},
		{name: "one day past the ceiling", lutealLength: maxPlausibleLutealPhaseDays + 1, wantRefined: false},
		{name: "at the floor", lutealLength: minLutealPhaseDays, wantRefined: true},
		{name: "one day below the floor", lutealLength: minLutealPhaseDays - 1, wantRefined: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			logs := eggWhiteLutealLogs(testCase.lutealLength)
			got, refined := InferUserLutealPhase(logs, time.UTC)
			if refined != testCase.wantRefined {
				t.Fatalf("InferUserLutealPhase reported refined=%v for a %d-day luteal phase, want %v", refined, testCase.lutealLength, testCase.wantRefined)
			}
			want := testCase.lutealLength
			if !testCase.wantRefined {
				want = defaultLutealPhaseDays
			}
			if got != want {
				t.Errorf("InferUserLutealPhase = %d for a %d-day luteal phase, want %d", got, testCase.lutealLength, want)
			}
		})
	}
}
