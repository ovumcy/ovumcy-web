package services

// stats_cycle_insights_coverage_test.go
// Mutation-hardening tests for internal/services/stats_cycle_insights.go.
// Every helper is prefixed statscycleinsightsCov to avoid collisions when
// multiple coverage agents write files into the same package.

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func statscycleinsightsCovDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("statscycleinsightsCovDay: parse %q: %v", raw, err)
	}
	return parsed
}

func statscycleinsightsCovLog(t *testing.T, day string, isPeriod bool, symptoms ...uint) models.DailyLog {
	t.Helper()
	return models.DailyLog{
		Date:       statscycleinsightsCovDay(t, day),
		IsPeriod:   isPeriod,
		SymptomIDs: symptoms,
	}
}

func statscycleinsightsCovBBTLog(t *testing.T, day string, bbt float64) models.DailyLog {
	t.Helper()
	return models.DailyLog{
		Date: statscycleinsightsCovDay(t, day),
		BBT:  models.NewBBT(bbt),
	}
}

func statscycleinsightsCovSymptomMap(syms ...models.SymptomType) map[uint]models.SymptomType {
	m := make(map[uint]models.SymptomType, len(syms))
	for _, s := range syms {
		m[s.ID] = s
	}
	return m
}

// ---------------------------------------------------------------------------
// buildCompletedCycleSpans – line 39: len(starts) < 2 boundary
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovBuildCompletedCycleSpansSingleStartReturnsNil checks that
// exactly one observed period start yields nil (the < 2 guard on line 39).
func TestStatscycleinsightsCovBuildCompletedCycleSpansSingleStartReturnsNil(t *testing.T) {
	logs := []models.DailyLog{
		statscycleinsightsCovLog(t, "2026-01-01", true),
		{Date: statscycleinsightsCovDay(t, "2026-01-03"), IsPeriod: false},
	}
	spans := buildCompletedCycleSpans(logs, time.UTC)
	if spans != nil {
		t.Fatalf("expected nil with a single cycle start, got %v", spans)
	}
}

// TestStatscycleinsightsCovBuildCompletedCycleSpansTwoStartsYieldsOneSpan checks that
// exactly two starts produce exactly one completed span (the < 2 boundary is not
// off-by-one; a mutant that changes to <= 2 would return nil instead).
func TestStatscycleinsightsCovBuildCompletedCycleSpansTwoStartsYieldsOneSpan(t *testing.T) {
	logs := []models.DailyLog{
		statscycleinsightsCovLog(t, "2026-01-01", true),
		statscycleinsightsCovLog(t, "2026-01-29", true),
	}
	spans := buildCompletedCycleSpans(logs, time.UTC)
	if len(spans) != 1 {
		t.Fatalf("expected exactly one completed span for two starts, got %d", len(spans))
	}
	if spans[0].CycleLength != 28 {
		t.Fatalf("expected cycle length 28, got %d", spans[0].CycleLength)
	}
}

// ---------------------------------------------------------------------------
// buildCompletedCycleSpans – line 46: loop boundary (index+1 < len(starts))
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovBuildCompletedCycleSpansCountMatchesStarts verifies that
// three starts produce exactly two spans (loop iterates index 0 and 1 only).
func TestStatscycleinsightsCovBuildCompletedCycleSpansCountMatchesStarts(t *testing.T) {
	logs := []models.DailyLog{
		statscycleinsightsCovLog(t, "2026-01-01", true),
		statscycleinsightsCovLog(t, "2026-01-29", true),
		statscycleinsightsCovLog(t, "2026-02-26", true),
	}
	spans := buildCompletedCycleSpans(logs, time.UTC)
	if len(spans) != 2 {
		t.Fatalf("expected two completed spans for three starts, got %d", len(spans))
	}
}

// ---------------------------------------------------------------------------
// buildCompletedCycleSpans – line 50: cycleLength <= 0 guard
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovBuildCompletedCycleSpansPositiveCycleLengthKept verifies
// that the cycleLength <= 0 guard on line 50 does not discard valid (positive)
// cycle lengths.  Two period starts exactly one day apart yield a cycleLength of 1
// which is > 0 and must be kept.
// Note: period days within 5 days of each other merge into a single cluster in
// buildPeriodClusters, so we need a gap >= 5 days to generate two distinct starts.
// A gap of exactly 6 days yields cycleLength=6.
func TestStatscycleinsightsCovBuildCompletedCycleSpansPositiveCycleLengthKept(t *testing.T) {
	// Gap of 6 days forces two distinct period clusters -> two observed starts.
	logs := []models.DailyLog{
		statscycleinsightsCovLog(t, "2026-01-01", true),
		statscycleinsightsCovLog(t, "2026-01-07", true),
	}
	spans := buildCompletedCycleSpans(logs, time.UTC)
	if len(spans) != 1 {
		t.Fatalf("expected one span for a 6-day cycle, got %d", len(spans))
	}
	if spans[0].CycleLength != 6 {
		t.Fatalf("expected cycle length 6, got %d", spans[0].CycleLength)
	}
}

// ---------------------------------------------------------------------------
// buildCompletedCycleSpans – line 55: periodLength <= 0 → DefaultPeriodLength
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovBuildCompletedCycleSpansPeriodLengthDefaultsWhenZero
// constructs a period start with no consecutive IsPeriod days so buildCycles
// assigns PeriodLength=0; the span should carry DefaultPeriodLength.
func TestStatscycleinsightsCovBuildCompletedCycleSpansPeriodLengthDefaultsWhenZero(t *testing.T) {
	// The period start log has IsPeriod=true but the following day does not,
	// so buildCycles computes periodLength=1 (the start day itself counts).
	// To force 0 we need a scenario where no day in the period window matches
	// IsPeriod=true. Looking at buildCycles: it counts consecutive IsPeriod days
	// starting at 'start'. If the start log itself is IsPeriod=true, periodLength>=1.
	// Only when IsPeriod is false at the start does it stay 0.
	// ObservedCycleStarts uses period clusters so start days always have IsPeriod.
	// The actual PeriodLength recorded in detectedCycle is therefore always >= 1.
	// This means line 55 ("if periodLength <= 0") is a defensive guard for future
	// code and is currently unreachable — marking as equivalent below.
	// We still verify that the fallback path does not corrupt span output when it
	// would be reached: we check that a normal span has a non-zero PeriodLength.
	logs := []models.DailyLog{
		{Date: statscycleinsightsCovDay(t, "2026-01-01"), IsPeriod: true},
		{Date: statscycleinsightsCovDay(t, "2026-01-29"), IsPeriod: true},
	}
	spans := buildCompletedCycleSpans(logs, time.UTC)
	if len(spans) != 1 {
		t.Fatalf("expected one span, got %d", len(spans))
	}
	if spans[0].PeriodLength <= 0 {
		t.Fatalf("expected positive period length, got %d", spans[0].PeriodLength)
	}
}

// ---------------------------------------------------------------------------
// buildLastCycleSymptomCounts – line 84: totalLoggedDays increment
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovLastCycleSymptomCountsTotalDaysCountedCorrectly checks
// that TotalDays on each item reflects the number of days with any log entry
// inside the last cycle window (line 84 increment).
func TestStatscycleinsightsCovLastCycleSymptomCountsTotalDaysCountedCorrectly(t *testing.T) {
	syms := statscycleinsightsCovSymptomMap(
		models.SymptomType{ID: 1, Name: "Cramps", Icon: "C"},
	)
	spans := []completedCycleSpan{
		{
			Start:     statscycleinsightsCovDay(t, "2026-01-29"),
			NextStart: statscycleinsightsCovDay(t, "2026-02-26"),
		},
	}
	logs := []models.DailyLog{
		// Three days inside the window, only one has a symptom.
		{Date: statscycleinsightsCovDay(t, "2026-01-30"), SymptomIDs: []uint{1}},
		{Date: statscycleinsightsCovDay(t, "2026-02-01")},
		{Date: statscycleinsightsCovDay(t, "2026-02-10")},
	}
	items := buildLastCycleSymptomCounts("en", logs, spans, syms, time.UTC)
	if len(items) != 1 {
		t.Fatalf("expected one item, got %d", len(items))
	}
	if items[0].TotalDays != 3 {
		t.Fatalf("expected TotalDays=3 (all days in window counted), got %d", items[0].TotalDays)
	}
	if items[0].Count != 1 {
		t.Fatalf("expected Count=1 for Cramps, got %d", items[0].Count)
	}
}

// ---------------------------------------------------------------------------
// buildLastCycleSymptomCounts – line 110: sort descending by count
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovLastCycleSymptomCountsSortsByCountDescending verifies
// that the item with the higher count appears first (line 110 sort comparator).
func TestStatscycleinsightsCovLastCycleSymptomCountsSortsByCountDescending(t *testing.T) {
	syms := statscycleinsightsCovSymptomMap(
		models.SymptomType{ID: 1, Name: "Cramps", Icon: "C"},
		models.SymptomType{ID: 2, Name: "Headache", Icon: "H"},
	)
	spans := []completedCycleSpan{
		{
			Start:     statscycleinsightsCovDay(t, "2026-01-29"),
			NextStart: statscycleinsightsCovDay(t, "2026-02-26"),
		},
	}
	logs := []models.DailyLog{
		// Cramps appears once, Headache appears twice.
		{Date: statscycleinsightsCovDay(t, "2026-01-30"), SymptomIDs: []uint{1, 2}},
		{Date: statscycleinsightsCovDay(t, "2026-02-01"), SymptomIDs: []uint{2}},
	}
	items := buildLastCycleSymptomCounts("en", logs, spans, syms, time.UTC)
	if len(items) != 2 {
		t.Fatalf("expected two items, got %d", len(items))
	}
	if items[0].Name != "Headache" || items[0].Count != 2 {
		t.Fatalf("expected Headache (count 2) first, got %s (count %d)", items[0].Name, items[0].Count)
	}
	if items[1].Name != "Cramps" || items[1].Count != 1 {
		t.Fatalf("expected Cramps (count 1) second, got %s (count %d)", items[1].Name, items[1].Count)
	}
}

// ---------------------------------------------------------------------------
// buildLastCycleSymptomCounts – line 108: sort tie-break by name
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovLastCycleSymptomCountsTieBreaksByName verifies that
// when two items share the same count they are ordered alphabetically (line 108).
func TestStatscycleinsightsCovLastCycleSymptomCountsTieBreaksByName(t *testing.T) {
	syms := statscycleinsightsCovSymptomMap(
		models.SymptomType{ID: 1, Name: "Zapping", Icon: "Z"},
		models.SymptomType{ID: 2, Name: "Acne", Icon: "A"},
	)
	spans := []completedCycleSpan{
		{
			Start:     statscycleinsightsCovDay(t, "2026-01-29"),
			NextStart: statscycleinsightsCovDay(t, "2026-02-26"),
		},
	}
	logs := []models.DailyLog{
		{Date: statscycleinsightsCovDay(t, "2026-01-30"), SymptomIDs: []uint{1, 2}},
	}
	items := buildLastCycleSymptomCounts("en", logs, spans, syms, time.UTC)
	if len(items) != 2 {
		t.Fatalf("expected two items, got %d", len(items))
	}
	// Equal counts: alphabetical → Acne before Zapping.
	if items[0].Name != "Acne" {
		t.Fatalf("expected Acne first on tie, got %s", items[0].Name)
	}
	if items[1].Name != "Zapping" {
		t.Fatalf("expected Zapping second on tie, got %s", items[1].Name)
	}
}

// ---------------------------------------------------------------------------
// buildLastCycleSymptomCounts – line 112: truncate to top 3
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovLastCycleSymptomCountsTruncatesToThree verifies that
// when more than three distinct symptoms are logged only the top three are returned.
func TestStatscycleinsightsCovLastCycleSymptomCountsTruncatesToThree(t *testing.T) {
	syms := statscycleinsightsCovSymptomMap(
		models.SymptomType{ID: 1, Name: "Acne", Icon: "A"},
		models.SymptomType{ID: 2, Name: "Bloating", Icon: "B"},
		models.SymptomType{ID: 3, Name: "Cramps", Icon: "C"},
		models.SymptomType{ID: 4, Name: "Headache", Icon: "H"},
	)
	spans := []completedCycleSpan{
		{
			Start:     statscycleinsightsCovDay(t, "2026-01-29"),
			NextStart: statscycleinsightsCovDay(t, "2026-02-26"),
		},
	}
	logs := []models.DailyLog{
		{Date: statscycleinsightsCovDay(t, "2026-01-30"), SymptomIDs: []uint{1, 2, 3, 4}},
	}
	items := buildLastCycleSymptomCounts("en", logs, spans, syms, time.UTC)
	if len(items) != 3 {
		t.Fatalf("expected items capped at 3, got %d", len(items))
	}
}

// TestStatscycleinsightsCovLastCycleSymptomCountsExactlyThreeNotTruncated verifies
// that exactly three items are returned in full when len==3 (> 3 guard, not >= 3).
func TestStatscycleinsightsCovLastCycleSymptomCountsExactlyThreeNotTruncated(t *testing.T) {
	syms := statscycleinsightsCovSymptomMap(
		models.SymptomType{ID: 1, Name: "Acne", Icon: "A"},
		models.SymptomType{ID: 2, Name: "Bloating", Icon: "B"},
		models.SymptomType{ID: 3, Name: "Cramps", Icon: "C"},
	)
	spans := []completedCycleSpan{
		{
			Start:     statscycleinsightsCovDay(t, "2026-01-29"),
			NextStart: statscycleinsightsCovDay(t, "2026-02-26"),
		},
	}
	logs := []models.DailyLog{
		{Date: statscycleinsightsCovDay(t, "2026-01-30"), SymptomIDs: []uint{1, 2, 3}},
	}
	items := buildLastCycleSymptomCounts("en", logs, spans, syms, time.UTC)
	if len(items) != 3 {
		t.Fatalf("expected exactly 3 items returned (not truncated), got %d", len(items))
	}
}

// ---------------------------------------------------------------------------
// buildSymptomPatternInsights – lines 148/151: dayStart/dayEnd tracking
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovSymptomPatternInsightsDayRangeTracked verifies that
// DayStart and DayEnd on a pattern reflect the earliest and latest cycle day on
// which the symptom was logged (lines 148 and 151).
func TestStatscycleinsightsCovSymptomPatternInsightsDayRangeTracked(t *testing.T) {
	// Three completed cycles needed for minimumPhaseInsightCycles.
	spans := []completedCycleSpan{
		{Start: statscycleinsightsCovDay(t, "2026-01-01"), NextStart: statscycleinsightsCovDay(t, "2026-01-29")},
		{Start: statscycleinsightsCovDay(t, "2026-01-29"), NextStart: statscycleinsightsCovDay(t, "2026-02-26")},
		{Start: statscycleinsightsCovDay(t, "2026-02-26"), NextStart: statscycleinsightsCovDay(t, "2026-03-26")},
	}
	syms := statscycleinsightsCovSymptomMap(
		models.SymptomType{ID: 1, Name: "Cramps", Icon: "C"},
	)
	logs := []models.DailyLog{
		// Cramps on cycle day 2 (2nd day of first span).
		{Date: statscycleinsightsCovDay(t, "2026-01-02"), SymptomIDs: []uint{1}},
		// Cramps on cycle day 10 (10th day of first span).
		{Date: statscycleinsightsCovDay(t, "2026-01-10"), SymptomIDs: []uint{1}},
		// Cramps on cycle day 5 (5th day of second span).
		{Date: statscycleinsightsCovDay(t, "2026-02-02"), SymptomIDs: []uint{1}},
	}

	items := buildSymptomPatternInsights(logs, spans, syms, time.UTC)
	if len(items) != 1 {
		t.Fatalf("expected one pattern item, got %d", len(items))
	}
	if items[0].DayStart != 2 {
		t.Fatalf("expected DayStart=2, got %d", items[0].DayStart)
	}
	if items[0].DayEnd != 10 {
		t.Fatalf("expected DayEnd=10, got %d", items[0].DayEnd)
	}
	if items[0].Count != 3 {
		t.Fatalf("expected Count=3, got %d", items[0].Count)
	}
}

// ---------------------------------------------------------------------------
// buildSymptomPatternInsights – line 173: sort descending by count
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovSymptomPatternInsightsSortsByCountDescending checks that
// the pattern with the higher occurrence count appears first (line 173).
func TestStatscycleinsightsCovSymptomPatternInsightsSortsByCountDescending(t *testing.T) {
	spans := []completedCycleSpan{
		{Start: statscycleinsightsCovDay(t, "2026-01-01"), NextStart: statscycleinsightsCovDay(t, "2026-01-29")},
		{Start: statscycleinsightsCovDay(t, "2026-01-29"), NextStart: statscycleinsightsCovDay(t, "2026-02-26")},
		{Start: statscycleinsightsCovDay(t, "2026-02-26"), NextStart: statscycleinsightsCovDay(t, "2026-03-26")},
	}
	syms := statscycleinsightsCovSymptomMap(
		models.SymptomType{ID: 1, Name: "Cramps", Icon: "C"},
		models.SymptomType{ID: 2, Name: "Headache", Icon: "H"},
	)
	logs := []models.DailyLog{
		// Cramps once, Headache twice.
		{Date: statscycleinsightsCovDay(t, "2026-01-02"), SymptomIDs: []uint{1, 2}},
		{Date: statscycleinsightsCovDay(t, "2026-01-10"), SymptomIDs: []uint{2}},
	}

	items := buildSymptomPatternInsights(logs, spans, syms, time.UTC)
	if len(items) != 2 {
		t.Fatalf("expected two pattern items, got %d", len(items))
	}
	if items[0].Name != "Headache" || items[0].Count != 2 {
		t.Fatalf("expected Headache (count 2) first, got %s (count %d)", items[0].Name, items[0].Count)
	}
}

// ---------------------------------------------------------------------------
// buildSymptomPatternInsights – line 171: NOT COVERED — name tie-break
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovSymptomPatternInsightsTieBreaksByName exercises line 171
// (the name-based tie-break inside the sort comparator for equal counts).
func TestStatscycleinsightsCovSymptomPatternInsightsTieBreaksByName(t *testing.T) {
	spans := []completedCycleSpan{
		{Start: statscycleinsightsCovDay(t, "2026-01-01"), NextStart: statscycleinsightsCovDay(t, "2026-01-29")},
		{Start: statscycleinsightsCovDay(t, "2026-01-29"), NextStart: statscycleinsightsCovDay(t, "2026-02-26")},
		{Start: statscycleinsightsCovDay(t, "2026-02-26"), NextStart: statscycleinsightsCovDay(t, "2026-03-26")},
	}
	syms := statscycleinsightsCovSymptomMap(
		models.SymptomType{ID: 1, Name: "Zapping", Icon: "Z"},
		models.SymptomType{ID: 2, Name: "Acne", Icon: "A"},
	)
	logs := []models.DailyLog{
		// Both symptoms appear exactly once — equal counts, tie-break by name.
		{Date: statscycleinsightsCovDay(t, "2026-01-02"), SymptomIDs: []uint{1, 2}},
	}

	items := buildSymptomPatternInsights(logs, spans, syms, time.UTC)
	if len(items) != 2 {
		t.Fatalf("expected two pattern items, got %d", len(items))
	}
	// Equal counts → alphabetical → Acne before Zapping.
	if items[0].Name != "Acne" {
		t.Fatalf("expected Acne first on name tie-break, got %s", items[0].Name)
	}
	if items[1].Name != "Zapping" {
		t.Fatalf("expected Zapping second on name tie-break, got %s", items[1].Name)
	}
}

// ---------------------------------------------------------------------------
// buildSymptomPatternInsights – line 175: truncate to top 2
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovSymptomPatternInsightsTruncatesToTwo verifies that more
// than two patterns are capped at two results (line 175 guard).
func TestStatscycleinsightsCovSymptomPatternInsightsTruncatesToTwo(t *testing.T) {
	spans := []completedCycleSpan{
		{Start: statscycleinsightsCovDay(t, "2026-01-01"), NextStart: statscycleinsightsCovDay(t, "2026-01-29")},
		{Start: statscycleinsightsCovDay(t, "2026-01-29"), NextStart: statscycleinsightsCovDay(t, "2026-02-26")},
		{Start: statscycleinsightsCovDay(t, "2026-02-26"), NextStart: statscycleinsightsCovDay(t, "2026-03-26")},
	}
	syms := statscycleinsightsCovSymptomMap(
		models.SymptomType{ID: 1, Name: "Acne", Icon: "A"},
		models.SymptomType{ID: 2, Name: "Bloating", Icon: "B"},
		models.SymptomType{ID: 3, Name: "Cramps", Icon: "C"},
	)
	logs := []models.DailyLog{
		{Date: statscycleinsightsCovDay(t, "2026-01-02"), SymptomIDs: []uint{1, 2, 3}},
	}

	items := buildSymptomPatternInsights(logs, spans, syms, time.UTC)
	if len(items) != 2 {
		t.Fatalf("expected patterns capped at 2, got %d", len(items))
	}
}

// TestStatscycleinsightsCovSymptomPatternInsightsExactlyTwoNotTruncated verifies
// that exactly two patterns pass through without truncation (> 2, not >= 2).
func TestStatscycleinsightsCovSymptomPatternInsightsExactlyTwoNotTruncated(t *testing.T) {
	spans := []completedCycleSpan{
		{Start: statscycleinsightsCovDay(t, "2026-01-01"), NextStart: statscycleinsightsCovDay(t, "2026-01-29")},
		{Start: statscycleinsightsCovDay(t, "2026-01-29"), NextStart: statscycleinsightsCovDay(t, "2026-02-26")},
		{Start: statscycleinsightsCovDay(t, "2026-02-26"), NextStart: statscycleinsightsCovDay(t, "2026-03-26")},
	}
	syms := statscycleinsightsCovSymptomMap(
		models.SymptomType{ID: 1, Name: "Acne", Icon: "A"},
		models.SymptomType{ID: 2, Name: "Bloating", Icon: "B"},
	)
	logs := []models.DailyLog{
		{Date: statscycleinsightsCovDay(t, "2026-01-02"), SymptomIDs: []uint{1, 2}},
	}

	items := buildSymptomPatternInsights(logs, spans, syms, time.UTC)
	if len(items) != 2 {
		t.Fatalf("expected exactly 2 items (not truncated at == 2), got %d", len(items))
	}
}

// ---------------------------------------------------------------------------
// resolveCurrentCycleBBTBounds – line 201: nil location fallback
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovResolveCurrentCycleBBTBoundsNilLocationFallsBackToUTC
// passes a nil location and verifies that it does not panic and returns valid
// bounds (line 201 nil-location guard).
func TestStatscycleinsightsCovResolveCurrentCycleBBTBoundsNilLocationFallsBackToUTC(t *testing.T) {
	periodStart := statscycleinsightsCovDay(t, "2026-01-01")
	stats := CycleStats{LastPeriodStart: periodStart}
	now := statscycleinsightsCovDay(t, "2026-01-10")

	cycleStart, today, ok := resolveCurrentCycleBBTBounds(stats, now, nil)
	if !ok {
		t.Fatalf("expected ok=true with nil location falling back to UTC")
	}
	if cycleStart.IsZero() {
		t.Fatalf("expected non-zero cycleStart with nil location")
	}
	if today.IsZero() {
		t.Fatalf("expected non-zero today with nil location")
	}
}

// ---------------------------------------------------------------------------
// buildCurrentCycleBBTSeries – line 237: minimum 5 recorded days
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovBuildCurrentCycleBBTSeriesRequiresFivePoints checks that
// fewer than five recorded days returns the empty/false sentinel (line 237).
func TestStatscycleinsightsCovBuildCurrentCycleBBTSeriesRequiresFivePoints(t *testing.T) {
	recorded := []int{1, 2, 3, 4}
	dayValues := map[int]float64{1: 36.5, 2: 36.5, 3: 36.5, 4: 36.5}
	labels, values, baseline, ok := buildCurrentCycleBBTSeries(recorded, dayValues)
	if ok || labels != nil || values != nil || baseline != 0 {
		t.Fatalf("expected empty result with fewer than 5 recorded days, got ok=%v labels=%v", ok, labels)
	}
}

// TestStatscycleinsightsCovBuildCurrentCycleBBTSeriesExactlyFiveSucceeds checks that
// exactly five recorded points are accepted (boundary is < 5, not <= 5).
func TestStatscycleinsightsCovBuildCurrentCycleBBTSeriesExactlyFiveSucceeds(t *testing.T) {
	recorded := []int{1, 2, 3, 4, 5}
	dayValues := map[int]float64{1: 36.4, 2: 36.5, 3: 36.45, 4: 36.42, 5: 36.48}
	_, _, baseline, ok := buildCurrentCycleBBTSeries(recorded, dayValues)
	if !ok {
		t.Fatalf("expected ok=true for exactly 5 recorded days")
	}
	// Baseline is average of first 5 values.
	expected := (36.4 + 36.5 + 36.45 + 36.42 + 36.48) / 5
	if diff := baseline - expected; diff < -0.0001 || diff > 0.0001 {
		t.Fatalf("expected baseline %.4f, got %.4f", expected, baseline)
	}
}

// ---------------------------------------------------------------------------
// buildCurrentCycleBBTSeries – line 245: label range covers 1..maxDay
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovBuildCurrentCycleBBTSeriesLabelsMatchMaxDay verifies that
// the returned label slice has length equal to the highest recorded day and that
// labels are the string representations of 1..maxDay (line 245 loop).
func TestStatscycleinsightsCovBuildCurrentCycleBBTSeriesLabelsMatchMaxDay(t *testing.T) {
	// Recorded days 1,3,5,7,9 — maxDay=9, so we expect 9 labels.
	recorded := []int{1, 3, 5, 7, 9}
	dayValues := map[int]float64{1: 36.4, 3: 36.45, 5: 36.5, 7: 36.52, 9: 36.55}
	labels, values, _, ok := buildCurrentCycleBBTSeries(recorded, dayValues)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if len(labels) != 9 {
		t.Fatalf("expected 9 labels (1..maxDay=9), got %d", len(labels))
	}
	if labels[0] != "1" || labels[8] != "9" {
		t.Fatalf("expected labels[0]=\"1\" and labels[8]=\"9\", got %q and %q", labels[0], labels[8])
	}
	if len(values) != 9 {
		t.Fatalf("expected 9 value slots, got %d", len(values))
	}
	// Slot for day 2 (index 1) should be nil (no recorded value).
	if values[1] != nil {
		t.Fatalf("expected nil value for unrecorded day 2")
	}
	// Slot for day 1 (index 0) should be non-nil.
	if values[0] == nil {
		t.Fatalf("expected non-nil value for recorded day 1")
	}
}

// ---------------------------------------------------------------------------
// detectProbableOvulationMarker – line 290: threshold = baseline + 0.2
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovDetectProbableOvulationMarkerThresholdExact verifies that
// three consecutive days where all values equal exactly baseline+0.2 are accepted
// (>= threshold, not > threshold) — and days just below are rejected.
func TestStatscycleinsightsCovDetectProbableOvulationMarkerThresholdExact(t *testing.T) {
	baseline := 36.40
	threshold := baseline + 0.2 // 36.60

	// Days 1..8 recorded; ovulation window at days 6,7,8 (index 5,6,7).
	recorded := []int{1, 2, 3, 4, 5, 6, 7, 8}
	dayValues := map[int]float64{
		1: 36.40, 2: 36.42, 3: 36.41, 4: 36.43, 5: 36.39,
		6: threshold, 7: threshold, 8: threshold,
	}
	_, ok := detectProbableOvulationMarker(recorded, dayValues, baseline)
	if !ok {
		t.Fatalf("expected ovulation marker detected when all three days meet threshold %.2f", threshold)
	}

	// One day just below threshold must suppress the marker.
	dayValues[7] = threshold - 0.001
	_, ok = detectProbableOvulationMarker(recorded, dayValues, baseline)
	if ok {
		t.Fatalf("expected no ovulation marker when day 7 is below threshold")
	}
}

// ---------------------------------------------------------------------------
// detectProbableOvulationMarker – line 291: loop starts at index 5
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovDetectProbableOvulationMarkerSkipsFirstFiveBaseline checks
// that a rising triple at days 3,4,5 (indices 2,3,4) — before the index=5 threshold
// — is not recognised as a marker. Only triples starting at or after index 5 count.
func TestStatscycleinsightsCovDetectProbableOvulationMarkerSkipsFirstFiveBaseline(t *testing.T) {
	baseline := 36.40
	threshold := baseline + 0.2

	// Eight days, rising triple at positions 3,4,5 (indices 2,3,4 — inside baseline window).
	recorded := []int{1, 2, 3, 4, 5, 6, 7, 8}
	dayValues := map[int]float64{
		1: 36.40, 2: 36.42,
		3: threshold, 4: threshold, 5: threshold,
		6: 36.40, 7: 36.40, 8: 36.40,
	}
	_, ok := detectProbableOvulationMarker(recorded, dayValues, baseline)
	if ok {
		t.Fatalf("expected no ovulation marker when triple is within the first-five baseline window")
	}
}

// ---------------------------------------------------------------------------
// detectProbableOvulationMarker – line 298: all three days must exceed threshold
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovDetectProbableOvulationMarkerAllThreeDaysMustExceedThreshold
// verifies that the marker is only emitted when dayOne, dayTwo, AND dayThree all
// meet the threshold (line 298 compound condition).
func TestStatscycleinsightsCovDetectProbableOvulationMarkerAllThreeDaysMustExceedThreshold(t *testing.T) {
	baseline := 36.40
	threshold := baseline + 0.2

	recorded := []int{1, 2, 3, 4, 5, 6, 7, 8}
	base := map[int]float64{
		1: 36.40, 2: 36.42, 3: 36.41, 4: 36.43, 5: 36.39,
		6: threshold, 7: threshold, 8: threshold,
	}

	// All three above: marker expected.
	_, ok := detectProbableOvulationMarker(recorded, copyDayValues(base), baseline)
	if !ok {
		t.Fatalf("expected marker when all three days are at threshold")
	}

	// Drop dayOne (day 6) below threshold.
	v6Low := copyDayValues(base)
	v6Low[6] = threshold - 0.01
	_, ok = detectProbableOvulationMarker(recorded, v6Low, baseline)
	if ok {
		t.Fatalf("expected no marker when first of the three days is below threshold")
	}

	// Drop dayThree (day 8) below threshold.
	v8Low := copyDayValues(base)
	v8Low[8] = threshold - 0.01
	_, ok = detectProbableOvulationMarker(recorded, v8Low, baseline)
	if ok {
		t.Fatalf("expected no marker when third of the three days is below threshold")
	}
}

// ---------------------------------------------------------------------------
// detectProbableOvulationMarker – line 303: markerDay < 1 guard
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovDetectProbableOvulationMarkerDayOneAtBoundary checks the
// edge case where the detected triple starts at the earliest possible position (day
// recorded[5] = 6) so that markerDay = dayOne-1 = 5, which is >= 1 and therefore
// the fallback (markerDay = dayOne) on line 304 is NOT taken.  We also verify the
// index returned is markerDay-1 (zero-based).
func TestStatscycleinsightsCovDetectProbableOvulationMarkerDayOneAtBoundary(t *testing.T) {
	baseline := 36.40
	threshold := baseline + 0.2

	recorded := []int{1, 2, 3, 4, 5, 6, 7, 8}
	dayValues := map[int]float64{
		1: 36.40, 2: 36.42, 3: 36.41, 4: 36.43, 5: 36.39,
		6: threshold, 7: threshold, 8: threshold,
	}
	// dayOne=6, markerDay = 6-1 = 5 (>= 1, no fallback).  Return index = 5-1 = 4.
	idx, ok := detectProbableOvulationMarker(recorded, dayValues, baseline)
	if !ok {
		t.Fatalf("expected marker detected")
	}
	if idx != 4 {
		t.Fatalf("expected marker index 4 (markerDay 5, zero-based), got %d", idx)
	}
}

// TestStatscycleinsightsCovDetectProbableOvulationMarkerMarkerDayFallbackDocumented
// documents that the markerDay < 1 branch (line 303-304) is unreachable in
// practice.  When recordedDays is sorted ascending and has >= 8 elements,
// recorded[5] (the 6th smallest day) is always >= 6, so dayOne-1 >= 5 >= 1 and
// the fallback never fires.  This test just exercises the normal marker detection
// path to ensure the index returned equals markerDay-1 in zero-based form.
func TestStatscycleinsightsCovDetectProbableOvulationMarkerMarkerDayFallbackDocumented(t *testing.T) {
	baseline := 36.40
	threshold := baseline + 0.2

	// dayOne = recorded[5] = 6, markerDay = 5, zero-based index = 4.
	recorded := []int{1, 2, 3, 4, 5, 6, 7, 8}
	dayValues := map[int]float64{
		1: 36.40, 2: 36.42, 3: 36.41, 4: 36.43, 5: 36.39,
		6: threshold, 7: threshold, 8: threshold,
	}
	idx, ok := detectProbableOvulationMarker(recorded, dayValues, baseline)
	if !ok {
		t.Fatalf("expected marker detected")
	}
	if idx != 4 {
		t.Fatalf("expected zero-based marker index 4, got %d", idx)
	}
	// The markerDay<1 branch is a defensive unreachable guard — it cannot be
	// triggered through the public API as documented in the classification notes.
}

// ---------------------------------------------------------------------------
// collectCurrentCycleBBTPoints – line 218: BBT filter
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovCollectCurrentCycleBBTPointsFiltersInvalidBBT checks that
// a log entry with BBT=0 (unset) is excluded from the collected points (line 218).
func TestStatscycleinsightsCovCollectCurrentCycleBBTPointsFiltersInvalidBBT(t *testing.T) {
	cycleStart := statscycleinsightsCovDay(t, "2026-01-01")
	today := statscycleinsightsCovDay(t, "2026-01-10")
	logs := []models.DailyLog{
		statscycleinsightsCovBBTLog(t, "2026-01-02", 36.5),
		statscycleinsightsCovBBTLog(t, "2026-01-03", 0), // unset — must be excluded
		statscycleinsightsCovBBTLog(t, "2026-01-04", 36.4),
	}
	recorded, dayValues := collectCurrentCycleBBTPoints(logs, cycleStart, today, time.UTC)
	if len(recorded) != 2 {
		t.Fatalf("expected 2 recorded days (0-BBT excluded), got %d", len(recorded))
	}
	if _, ok := dayValues[3]; ok {
		t.Fatalf("expected day 3 (BBT=0) to be excluded from dayValues")
	}
}

// TestStatscycleinsightsCovCollectCurrentCycleBBTPointsFiltersBeforeCycleStart checks
// that entries before cycleStart are excluded (line 218 Before(cycleStart) guard).
func TestStatscycleinsightsCovCollectCurrentCycleBBTPointsFiltersBeforeCycleStart(t *testing.T) {
	cycleStart := statscycleinsightsCovDay(t, "2026-01-05")
	today := statscycleinsightsCovDay(t, "2026-01-10")
	logs := []models.DailyLog{
		statscycleinsightsCovBBTLog(t, "2026-01-03", 36.5), // before cycleStart
		statscycleinsightsCovBBTLog(t, "2026-01-05", 36.4), // exactly cycleStart — included
		statscycleinsightsCovBBTLog(t, "2026-01-07", 36.6),
	}
	recorded, _ := collectCurrentCycleBBTPoints(logs, cycleStart, today, time.UTC)
	if len(recorded) != 2 {
		t.Fatalf("expected 2 recorded days (pre-cycle log excluded), got %d", len(recorded))
	}
}

// ---------------------------------------------------------------------------
// completedCycleDayNumber – helper used by buildSymptomPatternInsights
// ---------------------------------------------------------------------------

// TestStatscycleinsightsCovCompletedCycleDayNumberReturnsDayInFirstMatchingCycle
// verifies that completedCycleDayNumber returns the 1-based cycle day within the
// containing span and false for out-of-range dates.
func TestStatscycleinsightsCovCompletedCycleDayNumberReturnsDayInFirstMatchingCycle(t *testing.T) {
	spans := []completedCycleSpan{
		{Start: statscycleinsightsCovDay(t, "2026-01-01"), NextStart: statscycleinsightsCovDay(t, "2026-01-29")},
		{Start: statscycleinsightsCovDay(t, "2026-01-29"), NextStart: statscycleinsightsCovDay(t, "2026-02-26")},
	}

	// Day 1 of first span.
	day, ok := completedCycleDayNumber(statscycleinsightsCovDay(t, "2026-01-01"), spans, time.UTC)
	if !ok || day != 1 {
		t.Fatalf("expected day=1 ok=true, got day=%d ok=%v", day, ok)
	}

	// Day 5 of first span.
	day, ok = completedCycleDayNumber(statscycleinsightsCovDay(t, "2026-01-05"), spans, time.UTC)
	if !ok || day != 5 {
		t.Fatalf("expected day=5 ok=true, got day=%d ok=%v", day, ok)
	}

	// NextStart boundary: should belong to SECOND span as its day 1.
	day, ok = completedCycleDayNumber(statscycleinsightsCovDay(t, "2026-01-29"), spans, time.UTC)
	if !ok || day != 1 {
		t.Fatalf("expected second span day=1 ok=true, got day=%d ok=%v", day, ok)
	}

	// Before any span: false.
	_, ok = completedCycleDayNumber(statscycleinsightsCovDay(t, "2025-12-31"), spans, time.UTC)
	if ok {
		t.Fatalf("expected ok=false for date before all spans")
	}
}

// ---------------------------------------------------------------------------
// internal helper
// ---------------------------------------------------------------------------

func copyDayValues(src map[int]float64) map[int]float64 {
	dst := make(map[int]float64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
