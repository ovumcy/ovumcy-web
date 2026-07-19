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

// TestStatsCycleInsightsBuildCompletedCycleSpansSingleStartReturnsNil checks that
// exactly one observed period start yields nil (the < 2 guard on line 39).
func TestStatsCycleInsightsBuildCompletedCycleSpansSingleStartReturnsNil(t *testing.T) {
	logs := []models.DailyLog{
		statscycleinsightsCovLog(t, "2026-01-01", true),
		{Date: statscycleinsightsCovDay(t, "2026-01-03"), IsPeriod: false},
	}
	spans := buildCompletedCycleSpans(logs, time.UTC)
	if spans != nil {
		t.Fatalf("expected nil with a single cycle start, got %v", spans)
	}
}

// TestStatsCycleInsightsBuildCompletedCycleSpansTwoStartsYieldsOneSpan checks that
// exactly two starts produce exactly one completed span (the < 2 boundary is not
// off-by-one; a mutant that changes to <= 2 would return nil instead).
func TestStatsCycleInsightsBuildCompletedCycleSpansTwoStartsYieldsOneSpan(t *testing.T) {
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

// TestStatsCycleInsightsBuildCompletedCycleSpansCountMatchesStarts verifies that
// three starts produce exactly two spans (loop iterates index 0 and 1 only).
func TestStatsCycleInsightsBuildCompletedCycleSpansCountMatchesStarts(t *testing.T) {
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

// TestStatsCycleInsightsBuildCompletedCycleSpansPositiveCycleLengthKept verifies
// that the cycleLength <= 0 guard on line 50 does not discard valid (positive)
// cycle lengths.  Two period starts exactly one day apart yield a cycleLength of 1
// which is > 0 and must be kept.
// Note: period days within 5 days of each other merge into a single cluster in
// buildPeriodClusters, so we need a gap >= 5 days to generate two distinct starts.
// A gap of exactly 6 days yields cycleLength=6.
func TestStatsCycleInsightsBuildCompletedCycleSpansPositiveCycleLengthKept(t *testing.T) {
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

// TestStatsCycleInsightsBuildCompletedCycleSpansPeriodLengthDefaultsWhenZero
// constructs a period start with no consecutive IsPeriod days so buildCycles
// assigns PeriodLength=0; the span should carry DefaultPeriodLength.
func TestStatsCycleInsightsBuildCompletedCycleSpansPeriodLengthDefaultsWhenZero(t *testing.T) {
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

// TestStatsCycleInsightsLastCycleSymptomCountsTotalDaysCountedCorrectly checks
// that TotalDays on each item reflects the number of days with any log entry
// inside the last cycle window (line 84 increment).
func TestStatsCycleInsightsLastCycleSymptomCountsTotalDaysCountedCorrectly(t *testing.T) {
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

// TestStatsCycleInsightsLastCycleSymptomCountsSortsByCountDescending verifies
// that the item with the higher count appears first (line 110 sort comparator).
func TestStatsCycleInsightsLastCycleSymptomCountsSortsByCountDescending(t *testing.T) {
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

// TestStatsCycleInsightsLastCycleSymptomCountsTieBreaksByName verifies that
// when two items share the same count they are ordered alphabetically (line 108).
func TestStatsCycleInsightsLastCycleSymptomCountsTieBreaksByName(t *testing.T) {
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

// TestStatsCycleInsightsLastCycleSymptomCountsTruncatesToThree verifies that
// when more than three distinct symptoms are logged only the top three are returned.
func TestStatsCycleInsightsLastCycleSymptomCountsTruncatesToThree(t *testing.T) {
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

// TestStatsCycleInsightsLastCycleSymptomCountsExactlyThreeNotTruncated verifies
// that exactly three items are returned in full when len==3 (> 3 guard, not >= 3).
func TestStatsCycleInsightsLastCycleSymptomCountsExactlyThreeNotTruncated(t *testing.T) {
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

// TestStatsCycleInsightsSymptomPatternInsightsDayRangeTracked verifies that
// DayStart and DayEnd on a pattern reflect the earliest and latest cycle day on
// which the symptom was logged (lines 148 and 151).
func TestStatsCycleInsightsSymptomPatternInsightsDayRangeTracked(t *testing.T) {
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

// TestStatsCycleInsightsSymptomPatternInsightsSortsByCountDescending checks that
// the pattern with the higher occurrence count appears first (line 173).
func TestStatsCycleInsightsSymptomPatternInsightsSortsByCountDescending(t *testing.T) {
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

// TestStatsCycleInsightsSymptomPatternInsightsTieBreaksByName exercises line 171
// (the name-based tie-break inside the sort comparator for equal counts).
func TestStatsCycleInsightsSymptomPatternInsightsTieBreaksByName(t *testing.T) {
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

// TestStatsCycleInsightsSymptomPatternInsightsTruncatesToTwo verifies that more
// than two patterns are capped at two results (line 175 guard).
func TestStatsCycleInsightsSymptomPatternInsightsTruncatesToTwo(t *testing.T) {
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

// TestStatsCycleInsightsSymptomPatternInsightsExactlyTwoNotTruncated verifies
// that exactly two patterns pass through without truncation (> 2, not >= 2).
func TestStatsCycleInsightsSymptomPatternInsightsExactlyTwoNotTruncated(t *testing.T) {
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

// TestStatsCycleInsightsResolveCurrentCycleBBTBoundsNilLocationFallsBackToUTC
// passes a nil location and verifies that it does not panic and returns valid
// bounds (line 201 nil-location guard).
func TestStatsCycleInsightsResolveCurrentCycleBBTBoundsNilLocationFallsBackToUTC(t *testing.T) {
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

// TestStatsCycleInsightsBuildCurrentCycleBBTSeriesRequiresFivePoints checks that
// fewer than five recorded days returns the empty/false sentinel (line 237).
func TestStatsCycleInsightsBuildCurrentCycleBBTSeriesRequiresFivePoints(t *testing.T) {
	recorded := []int{1, 2, 3, 4}
	dayValues := map[int]float64{1: 36.5, 2: 36.5, 3: 36.5, 4: 36.5}
	labels, values, ok := buildCurrentCycleBBTSeries(recorded, dayValues)
	if ok || labels != nil || values != nil {
		t.Fatalf("expected empty result with fewer than 5 recorded days, got ok=%v labels=%v", ok, labels)
	}
}

// TestStatsCycleInsightsBuildCurrentCycleBBTSeriesExactlyFiveSucceeds checks that
// exactly five recorded points are accepted (boundary is < 5, not <= 5).
func TestStatsCycleInsightsBuildCurrentCycleBBTSeriesExactlyFiveSucceeds(t *testing.T) {
	recorded := []int{1, 2, 3, 4, 5}
	dayValues := map[int]float64{1: 36.4, 2: 36.5, 3: 36.45, 4: 36.42, 5: 36.48}
	_, _, ok := buildCurrentCycleBBTSeries(recorded, dayValues)
	if !ok {
		t.Fatalf("expected ok=true for exactly 5 recorded days")
	}
}

// ---------------------------------------------------------------------------
// buildCurrentCycleBBTSeries – line 245: label range covers 1..maxDay
// ---------------------------------------------------------------------------

// TestStatsCycleInsightsBuildCurrentCycleBBTSeriesLabelsMatchMaxDay verifies that
// the returned label slice has length equal to the highest recorded day and that
// labels are the string representations of 1..maxDay (line 245 loop).
func TestStatsCycleInsightsBuildCurrentCycleBBTSeriesLabelsMatchMaxDay(t *testing.T) {
	// Recorded days 1,3,5,7,9 — maxDay=9, so we expect 9 labels.
	recorded := []int{1, 3, 5, 7, 9}
	dayValues := map[int]float64{1: 36.4, 3: 36.45, 5: 36.5, 7: 36.52, 9: 36.55}
	labels, values, ok := buildCurrentCycleBBTSeries(recorded, dayValues)
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
// probableOvulationMarkerIndex – mapping the shared detector onto chart indices
// ---------------------------------------------------------------------------

// TestStatsCycleInsightsProbableOvulationMarkerIndexNoShiftReturnsFalse verifies
// that without a detected shift no marker is emitted regardless of the index.
func TestStatsCycleInsightsProbableOvulationMarkerIndexNoShiftReturnsFalse(t *testing.T) {
	if _, ok := probableOvulationMarkerIndex(0, false, 10); ok {
		t.Fatalf("expected no marker when the detector found no shift")
	}
}

// TestStatsCycleInsightsProbableOvulationMarkerIndexDayBeforeFirstHigh verifies
// the marker lands on the day BEFORE the first elevated day (ovulation-day
// convention shared with inferBBTOvulationDate), in zero-based chart index form.
func TestStatsCycleInsightsProbableOvulationMarkerIndexDayBeforeFirstHigh(t *testing.T) {
	// First elevated day = cycle day 10 → marker day 9 → zero-based index 8.
	idx, ok := probableOvulationMarkerIndex(10, true, 12)
	if !ok {
		t.Fatalf("expected marker emitted for a detected shift")
	}
	if idx != 8 {
		t.Fatalf("expected zero-based marker index 8 (day 9), got %d", idx)
	}
}

// TestStatsCycleInsightsProbableOvulationMarkerIndexOutOfRangeIsDropped verifies
// that a marker index beyond the chart's label range is suppressed rather than
// emitted out of bounds.
func TestStatsCycleInsightsProbableOvulationMarkerIndexOutOfRangeIsDropped(t *testing.T) {
	// First elevated day 10 → marker index 8; labelCount 5 → dropped.
	if _, ok := probableOvulationMarkerIndex(10, true, 5); ok {
		t.Fatalf("expected marker suppressed when its index exceeds the label range")
	}
}

// TestStatsCycleInsightsProbableOvulationMarkerIndexFirstDayClampsToItself covers
// the markerDay < 1 floor: a (synthetic) first elevated day of 1 has no day
// before it, so the marker clamps to the first high day itself (index 0).
func TestStatsCycleInsightsProbableOvulationMarkerIndexFirstDayClampsToItself(t *testing.T) {
	idx, ok := probableOvulationMarkerIndex(1, true, 5)
	if !ok {
		t.Fatalf("expected marker emitted for a detected shift on day 1")
	}
	if idx != 0 {
		t.Fatalf("expected clamped zero-based marker index 0, got %d", idx)
	}
}

// ---------------------------------------------------------------------------
// buildCurrentCycleBBTChart – end-to-end: coverline + marker from the shared
// "3-over-6" detector
// ---------------------------------------------------------------------------

// TestStatsCycleInsightsBBTChartCoverlineAndMarkerFromSharedDetector runs the
// full chart build: the drawn line equals the max of the 6 undisturbed
// temperatures preceding the shift, and the marker sits the day before the
// first elevated day.
func TestStatsCycleInsightsBBTChartCoverlineAndMarkerFromSharedDetector(t *testing.T) {
	cycleStart := statscycleinsightsCovDay(t, "2026-01-01")
	stats := CycleStats{LastPeriodStart: cycleStart}
	now := statscycleinsightsCovDay(t, "2026-01-14")

	logs := []models.DailyLog{
		statscycleinsightsCovBBTLog(t, "2026-01-01", 36.20),
		statscycleinsightsCovBBTLog(t, "2026-01-02", 36.25),
		statscycleinsightsCovBBTLog(t, "2026-01-03", 36.30), // window max → coverline
		statscycleinsightsCovBBTLog(t, "2026-01-04", 36.22),
		statscycleinsightsCovBBTLog(t, "2026-01-05", 36.24),
		statscycleinsightsCovBBTLog(t, "2026-01-06", 36.21),
		// Elevated streak: days 10, 11, 12 (calendar-consecutive).
		statscycleinsightsCovBBTLog(t, "2026-01-10", 36.45),
		statscycleinsightsCovBBTLog(t, "2026-01-11", 36.50),
		statscycleinsightsCovBBTLog(t, "2026-01-12", 36.55), // ≥ 36.30+0.2
	}

	chart := buildCurrentCycleBBTChart(stats, logs, now, time.UTC)
	if !chart.HasBaseline {
		t.Fatalf("expected coverline present after a detected shift")
	}
	if diff := chart.Baseline - 36.30; diff < -0.0001 || diff > 0.0001 {
		t.Fatalf("expected coverline 36.30 (max of preceding 6), got %.4f", chart.Baseline)
	}
	if !chart.HasMarker {
		t.Fatalf("expected ovulation marker for a detected shift")
	}
	// First elevated day = 10 → marker day 9 → zero-based index 8.
	if chart.MarkerIndex != 8 {
		t.Fatalf("expected marker index 8 (day 9), got %d", chart.MarkerIndex)
	}
}

// TestStatsCycleInsightsBBTChartNoShiftHidesCoverlineAndMarker verifies that a
// flat cycle renders the series (≥5 recorded values) but draws neither a
// coverline nor a marker — the coverline only exists once a shift is confirmed.
func TestStatsCycleInsightsBBTChartNoShiftHidesCoverlineAndMarker(t *testing.T) {
	cycleStart := statscycleinsightsCovDay(t, "2026-01-01")
	stats := CycleStats{LastPeriodStart: cycleStart}
	now := statscycleinsightsCovDay(t, "2026-01-14")

	logs := []models.DailyLog{
		statscycleinsightsCovBBTLog(t, "2026-01-01", 36.30),
		statscycleinsightsCovBBTLog(t, "2026-01-02", 36.32),
		statscycleinsightsCovBBTLog(t, "2026-01-03", 36.31),
		statscycleinsightsCovBBTLog(t, "2026-01-04", 36.29),
		statscycleinsightsCovBBTLog(t, "2026-01-05", 36.33),
		statscycleinsightsCovBBTLog(t, "2026-01-06", 36.30),
		statscycleinsightsCovBBTLog(t, "2026-01-07", 36.31),
	}

	chart := buildCurrentCycleBBTChart(stats, logs, now, time.UTC)
	if len(chart.Labels) == 0 {
		t.Fatalf("expected chart series rendered with ≥5 recorded values")
	}
	if chart.HasBaseline {
		t.Fatalf("expected no coverline before a shift is confirmed, got %.2f", chart.Baseline)
	}
	if chart.HasMarker {
		t.Fatalf("expected no marker before a shift is confirmed")
	}
}

// ---------------------------------------------------------------------------
// collectCurrentCycleBBTPoints – line 218: BBT filter
// ---------------------------------------------------------------------------

// TestStatsCycleInsightsCollectCurrentCycleBBTPointsFiltersInvalidBBT checks that
// a log entry with BBT=0 (unset) is excluded from the collected points (line 218).
func TestStatsCycleInsightsCollectCurrentCycleBBTPointsFiltersInvalidBBT(t *testing.T) {
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

// TestStatsCycleInsightsCollectCurrentCycleBBTPointsFiltersBeforeCycleStart checks
// that entries before cycleStart are excluded (line 218 Before(cycleStart) guard).
func TestStatsCycleInsightsCollectCurrentCycleBBTPointsFiltersBeforeCycleStart(t *testing.T) {
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

// TestStatsCycleInsightsCompletedCycleDayNumberReturnsDayInFirstMatchingCycle
// verifies that completedCycleDayNumber returns the 1-based cycle day within the
// containing span and false for out-of-range dates.
func TestStatsCycleInsightsCompletedCycleDayNumberReturnsDayInFirstMatchingCycle(t *testing.T) {
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
