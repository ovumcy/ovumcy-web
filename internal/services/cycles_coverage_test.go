package services

import (
	"math"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// cyclesCovDay is a helper that parses a YYYY-MM-DD string into a UTC midnight
// time.Time, failing the test on any parse error.
func cyclesCovDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		t.Fatalf("cyclesCovDay: parse %q: %v", s, err)
	}
	return d
}

// cyclesCovPeriodLog returns a DailyLog with IsPeriod=true for the given date.
func cyclesCovPeriodLog(t *testing.T, date string) models.DailyLog {
	t.Helper()
	return models.DailyLog{Date: cyclesCovDay(t, date), IsPeriod: true}
}

// --------------------------------------------------------------------------
// L63 – BuildCycleStats falls back to detectedStarts when ObservedCycleStarts
// returns empty (no logs with CycleStart=true or uncertain flags).
// --------------------------------------------------------------------------

// TestCyclesCov_BuildCycleStats_ObservedStartsFallback exercises the branch at
// line 63 where observedStarts is empty and the code falls back to using
// detectedStarts. The fallback is the only path that populates LastPeriodStart
// correctly in this scenario.
func TestCyclesCov_BuildCycleStats_ObservedStartsFallback(t *testing.T) {
	// Two plain period clusters – no CycleStart flag, no IsUncertain flag.
	// ObservedCycleStarts will find clusters but with no ExplicitStart and
	// HasUncertainExplicit=false, so it returns the cluster.Start values.
	// We need a case where ObservedCycleStarts returns nil: all clusters
	// having HasUncertainExplicit=true and no ExplicitStart.
	logs := []models.DailyLog{
		{Date: cyclesCovDay(t, "2025-03-01"), IsPeriod: true, CycleStart: true, IsUncertain: true},
		{Date: cyclesCovDay(t, "2025-03-02"), IsPeriod: true},
		// Second cluster.
		{Date: cyclesCovDay(t, "2025-03-29"), IsPeriod: true, CycleStart: true, IsUncertain: true},
		{Date: cyclesCovDay(t, "2025-03-30"), IsPeriod: true},
	}

	now := cyclesCovDay(t, "2025-04-05")
	stats := BuildCycleStats(logs, now)

	// Because ObservedCycleStarts skips uncertain-only clusters, it returns nil.
	// The fallback (line 63-65) assigns detectedStarts, so LastPeriodStart must
	// equal the last detected start (2025-03-29).
	if stats.LastPeriodStart.IsZero() {
		t.Fatal("expected non-zero LastPeriodStart from detectedStarts fallback")
	}
	if got := stats.LastPeriodStart.Format("2006-01-02"); got != "2025-03-29" {
		t.Fatalf("LastPeriodStart = %s, want 2025-03-29", got)
	}
}

// --------------------------------------------------------------------------
// L86, L88 – ResolveLutealPhase branch coverage (NOT COVERED paths)
// --------------------------------------------------------------------------

// TestCyclesCov_ResolveLutealPhase_ZeroReturnsDefault covers line 86
// (case value <= 0) and line 87 (return defaultLutealPhaseDays).
func TestCyclesCov_ResolveLutealPhase_ZeroReturnsDefault(t *testing.T) {
	for _, v := range []int{0, -1, -100} {
		if got := ResolveLutealPhase(v); got != defaultLutealPhaseDays {
			t.Fatalf("ResolveLutealPhase(%d) = %d, want %d (defaultLutealPhaseDays)",
				v, got, defaultLutealPhaseDays)
		}
	}
}

// TestCyclesCov_ResolveLutealPhase_BelowMinClampsToMin covers line 88
// (case value < minLutealPhaseDays) and line 89 (return minLutealPhaseDays).
func TestCyclesCov_ResolveLutealPhase_BelowMinClampsToMin(t *testing.T) {
	for _, v := range []int{1, 5, minLutealPhaseDays - 1} {
		if got := ResolveLutealPhase(v); got != minLutealPhaseDays {
			t.Fatalf("ResolveLutealPhase(%d) = %d, want %d (minLutealPhaseDays)",
				v, got, minLutealPhaseDays)
		}
	}
}

// --------------------------------------------------------------------------
// L100 – CalcOvulationDay threshold boundary: cycleLen exactly at the floor
// --------------------------------------------------------------------------

// TestCyclesCov_CalcOvulationDay_ExactFloorBoundary asserts the boundary
// between too-short (cycleLen = minLutealPhaseDays+minOvulationCycleDay-1)
// and just-valid (cycleLen = minLutealPhaseDays+minOvulationCycleDay).
func TestCyclesCov_CalcOvulationDay_ExactFloorBoundary(t *testing.T) {
	floor := minLutealPhaseDays + minOvulationCycleDay // 10+5 = 15

	// One below the floor: must return (0, false).
	if day, ok := CalcOvulationDay(floor-1, 14); day != 0 || ok {
		t.Fatalf("CalcOvulationDay(%d, 14) = (%d, %t), want (0, false)", floor-1, day, ok)
	}

	// Exactly at the floor: must return a valid (positive) ovulation day.
	// The exact flag may be false when the luteal phase was clamped; what
	// matters is that day > 0 (calculable).
	day, _ := CalcOvulationDay(floor, 14)
	if day <= 0 {
		t.Fatalf("CalcOvulationDay(%d, 14) = %d, want >0 (calculable at floor)", floor, day)
	}
}

// --------------------------------------------------------------------------
// L123 – PredictCycleWindow guard: zero periodStart or non-positive cycleLength
// --------------------------------------------------------------------------

// TestCyclesCov_PredictCycleWindow_ZeroPeriodStart exercises the
// periodStart.IsZero() branch at line 123.
func TestCyclesCov_PredictCycleWindow_ZeroPeriodStart(t *testing.T) {
	ovul, fs, fe, exact, calc := PredictCycleWindow(time.Time{}, 28, 14)
	if calc || exact {
		t.Fatalf("expected (false, false) for zero periodStart, got calc=%t exact=%t", calc, exact)
	}
	if !ovul.IsZero() || !fs.IsZero() || !fe.IsZero() {
		t.Fatalf("expected zero dates for zero periodStart")
	}
}

// TestCyclesCov_PredictCycleWindow_ZeroCycleLength exercises the
// cycleLength <= 0 branch at line 123.
func TestCyclesCov_PredictCycleWindow_ZeroCycleLength(t *testing.T) {
	periodStart := cyclesCovDay(t, "2026-03-01")
	ovul, fs, fe, exact, calc := PredictCycleWindow(periodStart, 0, 14)
	if calc || exact {
		t.Fatalf("expected (false, false) for zero cycleLength, got calc=%t exact=%t", calc, exact)
	}
	if !ovul.IsZero() || !fs.IsZero() || !fe.IsZero() {
		t.Fatalf("expected zero dates for zero cycleLength")
	}

	// Negative cycleLength must also be rejected.
	ovul2, _, _, _, calc2 := PredictCycleWindow(periodStart, -5, 14)
	if calc2 || !ovul2.IsZero() {
		t.Fatalf("expected non-calculable for negative cycleLength")
	}
}

// --------------------------------------------------------------------------
// L168 – DetectCycleStarts gap calculation: the -1 matters
// --------------------------------------------------------------------------

// TestCyclesCov_DetectCycleStarts_GapBoundary pins that a gap of exactly 4
// calendar days between consecutive period-logged days (which yields gapDays=3
// after the -1 subtraction) does NOT start a new cycle, while a gap of 6
// calendar days (gapDays=5) does.
func TestCyclesCov_DetectCycleStarts_GapBoundary(t *testing.T) {
	// Gap of exactly 5 calendar days: day.Sub(prev) = 5 days → gapDays = 4 → no new start.
	logs5 := []models.DailyLog{
		cyclesCovPeriodLog(t, "2026-01-01"),
		cyclesCovPeriodLog(t, "2026-01-06"), // 5 days later
	}
	starts5 := DetectCycleStarts(logs5)
	if len(starts5) != 1 {
		t.Fatalf("gap of 5 calendar days (gapDays=4): expected 1 start, got %d", len(starts5))
	}

	// Gap of exactly 6 calendar days: day.Sub(prev) = 6 days → gapDays = 5 → new start.
	logs6 := []models.DailyLog{
		cyclesCovPeriodLog(t, "2026-01-01"),
		cyclesCovPeriodLog(t, "2026-01-07"), // 6 days later
	}
	starts6 := DetectCycleStarts(logs6)
	if len(starts6) != 2 {
		t.Fatalf("gap of 6 calendar days (gapDays=5): expected 2 starts, got %d", len(starts6))
	}
}

// --------------------------------------------------------------------------
// L297 – populateObservedCycleStats: LastPeriodLength guard
// --------------------------------------------------------------------------

// TestCyclesCov_PopulateObservedCycleStats_LastPeriodLength verifies that
// LastPeriodLength is set to the last completed cycle's PeriodLength when
// completedCycleCount > 0 and len(cycles) >= completedCycleCount.
// The complementary case (completedCycleCount=0) should leave it as 0.
func TestCyclesCov_PopulateObservedCycleStats_LastPeriodLength(t *testing.T) {
	// Two complete cycles, period lengths 3 and 5.
	cycles := []detectedCycle{
		{Start: cyclesCovDay(t, "2025-01-01"), End: cyclesCovDay(t, "2025-01-28"), PeriodLength: 3},
		{Start: cyclesCovDay(t, "2025-01-29"), End: cyclesCovDay(t, "2025-02-25"), PeriodLength: 5},
	}
	lengths := []int{28, 28}

	var stats CycleStats
	populateObservedCycleStats(&stats, lengths, cycles)

	if stats.LastPeriodLength != 5 {
		t.Fatalf("LastPeriodLength = %d, want 5", stats.LastPeriodLength)
	}
	if stats.CompletedCycleCount != 2 {
		t.Fatalf("CompletedCycleCount = %d, want 2", stats.CompletedCycleCount)
	}

	// Zero lengths → LastPeriodLength stays 0.
	var stats2 CycleStats
	populateObservedCycleStats(&stats2, nil, cycles)
	if stats2.LastPeriodLength != 0 {
		t.Fatalf("expected LastPeriodLength=0 when no completed cycles, got %d", stats2.LastPeriodLength)
	}
}

// --------------------------------------------------------------------------
// L305 – recentPositivePeriodLengths: zero-length filter
// --------------------------------------------------------------------------

// TestCyclesCov_RecentPositivePeriodLengths_ZeroExcluded asserts that cycles
// with PeriodLength=0 are excluded from the returned slice, and thus don't
// influence AveragePeriodLength.
func TestCyclesCov_RecentPositivePeriodLengths_ZeroExcluded(t *testing.T) {
	cycles := []detectedCycle{
		{PeriodLength: 0},
		{PeriodLength: 4},
		{PeriodLength: 0},
		{PeriodLength: 6},
	}
	result := recentPositivePeriodLengths(cycles, 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 positive lengths, got %d: %v", len(result), result)
	}
	if result[0] != 4 || result[1] != 6 {
		t.Fatalf("expected [4 6], got %v", result)
	}
}

// --------------------------------------------------------------------------
// L348 – predictedPeriodLength: zero average falls back to default
// --------------------------------------------------------------------------

// TestCyclesCov_PredictedPeriodLength_ZeroAverageFallsBackToDefault exercises
// line 348 where length=0 triggers the fallback to models.DefaultPeriodLength.
func TestCyclesCov_PredictedPeriodLength_ZeroAverageFallsBackToDefault(t *testing.T) {
	got := predictedPeriodLength(0.0)
	if got != models.DefaultPeriodLength {
		t.Fatalf("predictedPeriodLength(0) = %d, want DefaultPeriodLength (%d)", got, models.DefaultPeriodLength)
	}
	// Positive average rounds correctly.
	if got := predictedPeriodLength(4.6); got != 5 {
		t.Fatalf("predictedPeriodLength(4.6) = %d, want 5", got)
	}
	if got := predictedPeriodLength(4.4); got != 4 {
		t.Fatalf("predictedPeriodLength(4.4) = %d, want 4", got)
	}
}

// --------------------------------------------------------------------------
// L418 – buildCycles: last cycle's End equals its Start (no next start)
// --------------------------------------------------------------------------

// TestCyclesCov_BuildCycles_LastCycleEndEqualsStart verifies that when there
// is only one start (no following start), the last cycle's End field equals
// its Start field (i.e., we don't panic or mutate it to next-1).
func TestCyclesCov_BuildCycles_LastCycleEndEqualsStart(t *testing.T) {
	starts := []time.Time{cyclesCovDay(t, "2026-01-01")}
	logs := []models.DailyLog{
		{Date: cyclesCovDay(t, "2026-01-01"), IsPeriod: true},
		{Date: cyclesCovDay(t, "2026-01-02"), IsPeriod: true},
	}
	cycles := buildCycles(starts, logs)
	if len(cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(cycles))
	}
	if !cycles[0].End.Equal(cycles[0].Start) {
		t.Fatalf("last cycle End %s != Start %s; want End=Start when no next cycle",
			cycles[0].End.Format("2006-01-02"), cycles[0].Start.Format("2006-01-02"))
	}
	// With two starts, the first cycle's End should be the day before the second start.
	starts2 := []time.Time{
		cyclesCovDay(t, "2026-01-01"),
		cyclesCovDay(t, "2026-01-29"),
	}
	cycles2 := buildCycles(starts2, logs)
	if len(cycles2) != 2 {
		t.Fatalf("expected 2 cycles, got %d", len(cycles2))
	}
	wantEnd := cyclesCovDay(t, "2026-01-28")
	if !cycles2[0].End.Equal(wantEnd) {
		t.Fatalf("first cycle End = %s, want 2026-01-28", cycles2[0].End.Format("2006-01-02"))
	}
}

// --------------------------------------------------------------------------
// L444 – cycleLengths: single start returns nil
// --------------------------------------------------------------------------

// TestCyclesCov_CycleLengths_SingleStartReturnsNil covers the guard at line
// 444 where len(starts) < 2 triggers an early nil return.
func TestCyclesCov_CycleLengths_SingleStartReturnsNil(t *testing.T) {
	single := []time.Time{cyclesCovDay(t, "2026-01-01")}
	if result := cycleLengths(single); result != nil {
		t.Fatalf("cycleLengths(single start) = %v, want nil", result)
	}
	empty := []time.Time{}
	if result := cycleLengths(empty); result != nil {
		t.Fatalf("cycleLengths(empty) = %v, want nil", result)
	}
	// Two starts should produce one length.
	two := []time.Time{
		cyclesCovDay(t, "2026-01-01"),
		cyclesCovDay(t, "2026-01-29"),
	}
	lengths := cycleLengths(two)
	if len(lengths) != 1 || lengths[0] != 28 {
		t.Fatalf("cycleLengths(two starts 28 apart) = %v, want [28]", lengths)
	}
}

// --------------------------------------------------------------------------
// L452, L455 – tailInts: NOT COVERED truncation path
// --------------------------------------------------------------------------

// TestCyclesCov_TailInts_TruncatesWhenExceedsN covers the branch at line 452
// (len > n) and the return at line 455.
func TestCyclesCov_TailInts_TruncatesWhenExceedsN(t *testing.T) {
	values := []int{10, 20, 30, 40, 50}

	// len(values)=5 > n=3: should return the last 3.
	tail := tailInts(values, 3)
	if len(tail) != 3 {
		t.Fatalf("tailInts len = %d, want 3", len(tail))
	}
	if tail[0] != 30 || tail[1] != 40 || tail[2] != 50 {
		t.Fatalf("tailInts = %v, want [30 40 50]", tail)
	}

	// len(values)=5 == n=5: unchanged.
	same := tailInts(values, 5)
	if len(same) != 5 || same[0] != 10 {
		t.Fatalf("tailInts unchanged path failed: %v", same)
	}

	// n=0: should return empty slice.
	none := tailInts(values, 0)
	if len(none) != 0 {
		t.Fatalf("tailInts(n=0) = %v, want []", none)
	}
}

// --------------------------------------------------------------------------
// L459, L462 – tailCycles: NOT COVERED truncation path
// --------------------------------------------------------------------------

// TestCyclesCov_TailCycles_TruncatesWhenExceedsN covers the branch at line
// 459 (len > n) and the return at line 462.
func TestCyclesCov_TailCycles_TruncatesWhenExceedsN(t *testing.T) {
	base := cyclesCovDay(t, "2026-01-01")
	cycles := make([]detectedCycle, 8)
	for i := range cycles {
		cycles[i] = detectedCycle{
			Start:        base.AddDate(0, 0, i*28),
			PeriodLength: i + 1,
		}
	}

	// 8 cycles, limit 6: should return the last 6.
	tail := tailCycles(cycles, 6)
	if len(tail) != 6 {
		t.Fatalf("tailCycles len = %d, want 6", len(tail))
	}
	if tail[0].PeriodLength != 3 {
		t.Fatalf("tailCycles[0].PeriodLength = %d, want 3", tail[0].PeriodLength)
	}
	if tail[5].PeriodLength != 8 {
		t.Fatalf("tailCycles[5].PeriodLength = %d, want 8", tail[5].PeriodLength)
	}

	// Exactly at limit: unchanged.
	same := tailCycles(cycles, 8)
	if len(same) != 8 {
		t.Fatalf("tailCycles same path: len = %d, want 8", len(same))
	}
}

// --------------------------------------------------------------------------
// L484, L487 – minMaxInts: min/max not at index 0
// --------------------------------------------------------------------------

// TestCyclesCov_MinMaxInts_MinAndMaxNotAtFirstPosition ensures the update
// branches at lines 484 and 487 are exercised and that the returned min/max
// reflect the actual extremes regardless of position.
func TestCyclesCov_MinMaxInts_MinAndMaxNotAtFirstPosition(t *testing.T) {
	// Descending: min is last, max is first.
	desc := []int{30, 25, 20, 15}
	mn, mx := minMaxInts(desc)
	if mn != 15 {
		t.Fatalf("min = %d, want 15", mn)
	}
	if mx != 30 {
		t.Fatalf("max = %d, want 30", mx)
	}

	// Ascending: min is first, max is last.
	asc := []int{10, 20, 35}
	mn2, mx2 := minMaxInts(asc)
	if mn2 != 10 {
		t.Fatalf("min = %d, want 10", mn2)
	}
	if mx2 != 35 {
		t.Fatalf("max = %d, want 35", mx2)
	}

	// Mixed: neither first nor last is the extremum.
	mixed := []int{20, 5, 40, 15}
	mn3, mx3 := minMaxInts(mixed)
	if mn3 != 5 {
		t.Fatalf("min = %d, want 5", mn3)
	}
	if mx3 != 40 {
		t.Fatalf("max = %d, want 40", mx3)
	}

	// Single element: min == max.
	single := []int{7}
	mn4, mx4 := minMaxInts(single)
	if mn4 != 7 || mx4 != 7 {
		t.Fatalf("single element: min=%d max=%d, want both 7", mn4, mx4)
	}
}

// --------------------------------------------------------------------------
// L495 – CycleLengthSpread guard conditions
// --------------------------------------------------------------------------

// TestCyclesCov_CycleLengthSpread_GuardConditions exercises every branch of
// the guard at line 495: zero min, zero max, and max < min all return 0.
func TestCyclesCov_CycleLengthSpread_GuardConditions(t *testing.T) {
	cases := []struct {
		name string
		min  int
		max  int
		want int
	}{
		{"zero min returns 0", 0, 30, 0},
		{"zero max returns 0", 28, 0, 0},
		{"both zero returns 0", 0, 0, 0},
		{"max < min returns 0", 30, 20, 0},
		{"valid spread", 21, 35, 14},
		{"equal min max", 28, 28, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stats := CycleStats{MinCycleLength: tc.min, MaxCycleLength: tc.max}
			if got := CycleLengthSpread(stats); got != tc.want {
				t.Fatalf("CycleLengthSpread(min=%d,max=%d) = %d, want %d", tc.min, tc.max, got, tc.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// L502 – IsIrregularCycleSpread boundary at irregularCycleSpreadDays (7)
// --------------------------------------------------------------------------

// TestCyclesCov_IsIrregularCycleSpread_Boundary pins that a spread of exactly
// irregularCycleSpreadDays (7) is NOT irregular, while 8 IS.
func TestCyclesCov_IsIrregularCycleSpread_Boundary(t *testing.T) {
	atBound := CycleStats{MinCycleLength: 21, MaxCycleLength: 21 + irregularCycleSpreadDays}
	if IsIrregularCycleSpread(atBound) {
		t.Fatalf("spread == %d should not be irregular (requires > %d)", irregularCycleSpreadDays, irregularCycleSpreadDays)
	}

	pastBound := CycleStats{MinCycleLength: 21, MaxCycleLength: 21 + irregularCycleSpreadDays + 1}
	if !IsIrregularCycleSpread(pastBound) {
		t.Fatalf("spread == %d should be irregular (> %d)", irregularCycleSpreadDays+1, irregularCycleSpreadDays)
	}

	belowBound := CycleStats{MinCycleLength: 25, MaxCycleLength: 28}
	if IsIrregularCycleSpread(belowBound) {
		t.Fatalf("spread == 3 should not be irregular")
	}
}

// --------------------------------------------------------------------------
// L532 – sameDay: different days return false
// --------------------------------------------------------------------------

// TestCyclesCov_SameDay pins that sameDay returns true only for dates with
// identical YYYY-MM-DD, regardless of time-of-day, and false otherwise.
func TestCyclesCov_SameDay(t *testing.T) {
	a := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	b := time.Date(2026, 3, 15, 23, 59, 59, 0, time.UTC)
	if !sameDay(a, b) {
		t.Fatal("expected sameDay=true for same calendar day with different times")
	}

	c := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	if sameDay(a, c) {
		t.Fatal("expected sameDay=false for different calendar days")
	}

	// Adjacent months.
	jan := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if sameDay(jan, feb) {
		t.Fatal("expected sameDay=false for Jan 31 vs Feb 1")
	}
}

// --------------------------------------------------------------------------
// L547 – filterLogsNotAfter: cutoff.IsZero() returns the original slice
// --------------------------------------------------------------------------

// TestCyclesCov_FilterLogsNotAfter_ZeroCutoffReturnsAll exercises the
// cutoff.IsZero() branch at line 547. A zero cutoff must return all logs
// unfiltered.
func TestCyclesCov_FilterLogsNotAfter_ZeroCutoffReturnsAll(t *testing.T) {
	logs := []models.DailyLog{
		{Date: cyclesCovDay(t, "2026-01-01"), IsPeriod: true},
		{Date: cyclesCovDay(t, "2026-06-01"), IsPeriod: true},
	}
	result := filterLogsNotAfter(logs, time.Time{})
	if len(result) != 2 {
		t.Fatalf("filterLogsNotAfter(zeroCutoff) = %d logs, want 2", len(result))
	}

	// Normal filtering: cutoff excludes future logs.
	cutoff := cyclesCovDay(t, "2026-03-01")
	filtered := filterLogsNotAfter(logs, cutoff)
	if len(filtered) != 1 {
		t.Fatalf("filterLogsNotAfter(cutoff=2026-03-01) = %d logs, want 1", len(filtered))
	}
	if filtered[0].Date.Format("2006-01-02") != "2026-01-01" {
		t.Fatalf("unexpected log date after filter: %s", filtered[0].Date.Format("2006-01-02"))
	}

	// Empty log slice with zero cutoff returns the same empty slice.
	empty := filterLogsNotAfter(nil, time.Time{})
	if empty != nil {
		t.Fatalf("filterLogsNotAfter(nil, zero) should return nil, got %v", empty)
	}
}

// --------------------------------------------------------------------------
// L562 – stddevInts: empty input returns 0
// --------------------------------------------------------------------------

// TestCyclesCov_StddevInts_EmptyReturnsZero covers the guard at line 562.
func TestCyclesCov_StddevInts_EmptyReturnsZero(t *testing.T) {
	if got := stddevInts(nil); got != 0 {
		t.Fatalf("stddevInts(nil) = %v, want 0", got)
	}
	if got := stddevInts([]int{}); got != 0 {
		t.Fatalf("stddevInts([]) = %v, want 0", got)
	}
}

// --------------------------------------------------------------------------
// L569, L570, L572 – stddevInts: formula correctness
// --------------------------------------------------------------------------

// TestCyclesCov_StddevInts_Formula pins the sample standard deviation
// (n-1 denominator) calculation against known values. Observed cycle
// lengths are a small sample of an ongoing process, so the sample
// estimator is used; fewer than two values have no defined spread.
func TestCyclesCov_StddevInts_Formula(t *testing.T) {
	// Single element: spread is undefined, must return 0.
	if got := stddevInts([]int{42}); got != 0 {
		t.Fatalf("stddevInts([42]) = %v, want 0", got)
	}

	// [2, 4, 4, 4, 5, 5, 7, 9]: squared diffs sum to 32, n-1 = 7,
	// sample stddev = sqrt(32/7).
	values := []int{2, 4, 4, 4, 5, 5, 7, 9}
	got := stddevInts(values)
	want := math.Sqrt(32.0 / 7.0)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("stddevInts(%v) = %.10f, want %.10f", values, got, want)
	}

	// Uniform slice: stddev must be 0.
	uniform := []int{5, 5, 5, 5}
	if got := stddevInts(uniform); got != 0 {
		t.Fatalf("stddevInts(uniform) = %v, want 0", got)
	}

	// Two values [28, 30]: mean=29, squared diffs sum to 2, n-1 = 1,
	// sample stddev = sqrt(2).
	two := []int{28, 30}
	gotTwo := stddevInts(two)
	if math.Abs(gotTwo-math.Sqrt2) > 1e-9 {
		t.Fatalf("stddevInts([28,30]) = %.10f, want sqrt(2)", gotTwo)
	}
}

// --------------------------------------------------------------------------
// L282, L293 – populateObservedCycleStats: empty-slice branches
// (classified equivalent, but tested here to confirm the zero-value behavior)
// --------------------------------------------------------------------------

// TestCyclesCov_PopulateObservedCycleStats_EmptyLeavesFieldsZero confirms
// that when both lengths and cycles are nil, all derived stats remain at zero
// (i.e., mutating the guard at L282 or L293 would be equivalent because
// averageInts(nil) and medianInt(nil) both return 0).
func TestCyclesCov_PopulateObservedCycleStats_EmptyLeavesFieldsZero(t *testing.T) {
	var stats CycleStats
	populateObservedCycleStats(&stats, nil, nil)
	if stats.AverageCycleLength != 0 || stats.MedianCycleLength != 0 || stats.AveragePeriodLength != 0 {
		t.Fatalf("expected all zero stats for empty input, got avg=%.1f med=%d avgPeriod=%.1f",
			stats.AverageCycleLength, stats.MedianCycleLength, stats.AveragePeriodLength)
	}
}
