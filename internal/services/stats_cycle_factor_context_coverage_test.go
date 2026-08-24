package services

import (
	"math"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// statscyclefactorcontextCovDay parses a "2006-01-02" string in UTC.
func statscyclefactorcontextCovDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", s, err)
	}
	return d
}

// statscyclefactorcontextCovLog returns a DailyLog dated at the given day
// (UTC midnight) with the given factor keys.
func statscyclefactorcontextCovLog(day time.Time, factorKeys ...string) models.DailyLog {
	return models.DailyLog{Date: day, CycleFactorKeys: factorKeys}
}

// ---------------------------------------------------------------------------
// Line 83 – window-start boundary (statsCycleFactorContextWindowDays - 1)
// ---------------------------------------------------------------------------

// TestCollectStatsCycleFactorCountsWindowBoundary verifies that a log entry
// exactly on windowStart (today - 89 days) is INCLUDED while a log entry one
// day earlier (today - 90 days) is EXCLUDED.
//
// Survives mutant: changing -(statsCycleFactorContextWindowDays-1) to
// -statsCycleFactorContextWindowDays shifts the window by one day, causing the
// "on-boundary" entry to be dropped.
func TestCollectStatsCycleFactorCountsWindowBoundary(t *testing.T) {
	now := statscyclefactorcontextCovDay(t, "2026-04-10")
	// windowStart = today - 89 days
	onBoundary := now.AddDate(0, 0, -(statsCycleFactorContextWindowDays - 1))
	justOutside := onBoundary.AddDate(0, 0, -1)

	logs := []models.DailyLog{
		statscyclefactorcontextCovLog(onBoundary, models.CycleFactorStress),
		statscyclefactorcontextCovLog(justOutside, models.CycleFactorTravel),
	}

	counts := collectStatsCycleFactorCounts(logs, now, time.UTC)

	if counts[models.CycleFactorStress] != 1 {
		t.Errorf("expected on-boundary stress count=1, got %d", counts[models.CycleFactorStress])
	}
	if counts[models.CycleFactorTravel] != 0 {
		t.Errorf("expected outside-boundary travel count=0, got %d", counts[models.CycleFactorTravel])
	}
}

// ---------------------------------------------------------------------------
// Line 106 – snapshot End = NextStart - 1 day
// ---------------------------------------------------------------------------

// TestBuildStatsCycleFactorSnapshotsEndIsOneDayBeforeNextStart verifies that
// the End field of each snapshot is NextStart minus one day.
//
// Survives mutant: removing AddDate(0,0,-1) would set End == NextStart.
func TestBuildStatsCycleFactorSnapshotsEndIsOneDayBeforeNextStart(t *testing.T) {
	start := statscyclefactorcontextCovDay(t, "2026-01-01")
	nextStart := statscyclefactorcontextCovDay(t, "2026-01-29")
	expectedEnd := nextStart.AddDate(0, 0, -1)

	cycle := completedCycleSpan{
		Start:       start,
		NextStart:   nextStart,
		CycleLength: 28,
	}

	// Provide a log entry inside the span.
	logs := []models.DailyLog{
		statscyclefactorcontextCovLog(
			statscyclefactorcontextCovDay(t, "2026-01-10"),
			models.CycleFactorStress,
		),
	}

	stats := CycleStats{MedianCycleLength: 28}
	snapshots := buildStatsCycleFactorSnapshots(logs, []completedCycleSpan{cycle}, stats, time.UTC)

	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(snapshots))
	}
	if !snapshots[0].End.Equal(expectedEnd) {
		t.Errorf("expected End=%s, got %s", expectedEnd.Format("2006-01-02"), snapshots[0].End.Format("2006-01-02"))
	}
}

// ---------------------------------------------------------------------------
// Lines 142 + 146 + 148 – classifyStatsCycleFactorComparison
// Lines 146 and 148 are NOT COVERED at all.
// ---------------------------------------------------------------------------

// TestClassifyStatsCycleFactorComparisonShorterAndLonger exercises both the
// "shorter" (line 146) and "longer" (line 148) branches, which had zero
// coverage, and verifies the exact returned string.
func TestClassifyStatsCycleFactorComparisonShorterAndLonger(t *testing.T) {
	const baseline = 28

	cases := []struct {
		name        string
		cycleLength int
		want        string
	}{
		// cycleLength <= baseline - 2 → "shorter"
		{name: "two below baseline", cycleLength: baseline - statsCycleFactorComparisonDelta, want: "shorter"},
		{name: "well below baseline", cycleLength: baseline - 10, want: "shorter"},
		// cycleLength >= baseline + 2 → "longer"
		{name: "two above baseline", cycleLength: baseline + statsCycleFactorComparisonDelta, want: "longer"},
		{name: "well above baseline", cycleLength: baseline + 10, want: "longer"},
		// within delta → "variable"
		{name: "one below baseline", cycleLength: baseline - 1, want: "variable"},
		{name: "exactly baseline", cycleLength: baseline, want: "variable"},
		{name: "one above baseline", cycleLength: baseline + 1, want: "variable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stats := CycleStats{MedianCycleLength: baseline}
			got := classifyStatsCycleFactorComparison(stats, tc.cycleLength)
			if got != tc.want {
				t.Errorf("classifyStatsCycleFactorComparison(median=%d, cycleLen=%d) = %q, want %q",
					baseline, tc.cycleLength, got, tc.want)
			}
		})
	}
}

// TestClassifyStatsCycleFactorComparisonWithNoBaselineAtAll pins the `baseline
// > 0` guards on both arms. With neither a median nor an average the baseline
// resolves to 0, and every cycle length must classify as "variable" — without
// the guards a very negative length would satisfy `<= baseline-delta` and a
// large one `>= baseline+delta`, so an account with no cycle history would read
// as a firm "shorter" or "longer".
//
// This case came from a round-three mutation file whose other four tests
// restated rows of the table above (26 → shorter, 30 → longer, 28 → variable);
// it is the one input that table did not carry, so it moved here and the file
// was removed.
func TestClassifyStatsCycleFactorComparisonWithNoBaselineAtAll(t *testing.T) {
	stats := CycleStats{MedianCycleLength: 0, AverageCycleLength: 0}

	if got := classifyStatsCycleFactorComparison(stats, -5); got != "variable" {
		t.Errorf("expected variable for a negative cycle length with no baseline, got %q", got)
	}
	if got := classifyStatsCycleFactorComparison(stats, 100); got != "variable" {
		t.Errorf("expected variable for a large cycle length with no baseline, got %q", got)
	}
}

// TestClassifyStatsCycleFactorComparisonFallsBackToAverage verifies that when
// MedianCycleLength is zero the function uses AverageCycleLength as the
// baseline (line 142–143 fallback).
//
// Survives mutant: if the baseline assignment on line 142 were changed so
// the fallback is skipped, a median=0 + average=28 input would produce
// "variable" for all cycle lengths instead of the correct "shorter"/"longer".
func TestClassifyStatsCycleFactorComparisonFallsBackToAverage(t *testing.T) {
	stats := CycleStats{
		MedianCycleLength:  0,
		AverageCycleLength: 28.0,
	}

	// cycleLength = 26 → 26 <= 28 - 2 = 26 → "shorter"
	got := classifyStatsCycleFactorComparison(stats, 26)
	if got != "shorter" {
		t.Errorf("expected fallback-to-average to classify 26 as shorter (baseline 28), got %q", got)
	}

	// cycleLength = 30 → 30 >= 28 + 2 = 30 → "longer"
	got = classifyStatsCycleFactorComparison(stats, 30)
	if got != "longer" {
		t.Errorf("expected fallback-to-average to classify 30 as longer (baseline 28), got %q", got)
	}
}

// TestClassifyStatsCycleFactorComparisonAverageRounding verifies that the
// average is rounded correctly before use (math.Round, not truncation).
func TestClassifyStatsCycleFactorComparisonAverageRounding(t *testing.T) {
	// AverageCycleLength 27.6 rounds to 28.
	stats := CycleStats{
		MedianCycleLength:  0,
		AverageCycleLength: 27.6,
	}
	rounded := int(math.Round(27.6)) // 28

	// cycleLength = rounded - 2 → should be "shorter"
	got := classifyStatsCycleFactorComparison(stats, rounded-statsCycleFactorComparisonDelta)
	if got != "shorter" {
		t.Errorf("expected rounding-to-28 and shorter result, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Line 179 – pattern item truncation at exactly the limit
// ---------------------------------------------------------------------------

// TestBuildStatsCycleFactorPatternSummariesExactlyAtItemLimit verifies that a
// pattern bucket with exactly statsCycleFactorPatternItemLimit items (2) is NOT
// truncated (len check is >, not >=).
//
// Survives mutant: changing > to >= would drop the second item when count==2.
func TestBuildStatsCycleFactorPatternSummariesExactlyAtItemLimit(t *testing.T) {
	// Two distinct factor keys in the "longer" bucket – exactly the limit.
	snapshots := []statsCycleFactorCycleSnapshot{
		{ComparisonKind: "longer", FactorKeys: []string{models.CycleFactorStress}},
		{ComparisonKind: "longer", FactorKeys: []string{models.CycleFactorTravel}},
	}

	summaries := buildStatsCycleFactorPatternSummaries(snapshots)

	var longerSummary *StatsCycleFactorPatternSummary
	for i := range summaries {
		if summaries[i].Kind == "longer" {
			longerSummary = &summaries[i]
			break
		}
	}
	if longerSummary == nil {
		t.Fatal("expected a 'longer' pattern summary")
	}
	if len(longerSummary.Items) != statsCycleFactorPatternItemLimit {
		t.Errorf("expected %d items at exact limit, got %d", statsCycleFactorPatternItemLimit, len(longerSummary.Items))
	}
}

// TestBuildStatsCycleFactorPatternSummariesTruncatesAboveLimit verifies that a
// bucket with more than the limit is truncated to statsCycleFactorPatternItemLimit.
func TestBuildStatsCycleFactorPatternSummariesTruncatesAboveLimit(t *testing.T) {
	// Three distinct factor keys in the "shorter" bucket – one over the limit.
	snapshots := []statsCycleFactorCycleSnapshot{
		{ComparisonKind: "shorter", FactorKeys: []string{models.CycleFactorStress}},
		{ComparisonKind: "shorter", FactorKeys: []string{models.CycleFactorTravel}},
		{ComparisonKind: "shorter", FactorKeys: []string{models.CycleFactorIllness}},
	}

	summaries := buildStatsCycleFactorPatternSummaries(snapshots)

	var shorterSummary *StatsCycleFactorPatternSummary
	for i := range summaries {
		if summaries[i].Kind == "shorter" {
			shorterSummary = &summaries[i]
			break
		}
	}
	if shorterSummary == nil {
		t.Fatal("expected a 'shorter' pattern summary")
	}
	if len(shorterSummary.Items) != statsCycleFactorPatternItemLimit {
		t.Errorf("expected truncation to %d items, got %d", statsCycleFactorPatternItemLimit, len(shorterSummary.Items))
	}
}

// ---------------------------------------------------------------------------
// Line 200 – recent-cycles truncation at exactly the limit
// ---------------------------------------------------------------------------

// TestBuildStatsCycleFactorRecentCyclesExactlyAtLimit verifies that exactly
// statsCycleFactorRecentCycleLimit (3) snapshots are all retained (not
// truncated – the check is >, not >=).
//
// Survives mutant: changing > to >= would drop one snapshot when count==3.
func TestBuildStatsCycleFactorRecentCyclesExactlyAtLimit(t *testing.T) {
	snapshots := []statsCycleFactorCycleSnapshot{
		{Start: statscyclefactorcontextCovDay(t, "2026-03-01"), FactorKeys: []string{models.CycleFactorStress}},
		{Start: statscyclefactorcontextCovDay(t, "2026-02-01"), FactorKeys: []string{models.CycleFactorTravel}},
		{Start: statscyclefactorcontextCovDay(t, "2026-01-01"), FactorKeys: []string{models.CycleFactorIllness}},
	}

	summaries := buildStatsCycleFactorRecentCycles(snapshots)

	if len(summaries) != statsCycleFactorRecentCycleLimit {
		t.Errorf("expected %d recent cycles at exact limit, got %d", statsCycleFactorRecentCycleLimit, len(summaries))
	}
}

// TestBuildStatsCycleFactorRecentCyclesTruncatesAboveLimit verifies that more
// than statsCycleFactorRecentCycleLimit snapshots are trimmed.
func TestBuildStatsCycleFactorRecentCyclesTruncatesAboveLimit(t *testing.T) {
	snapshots := []statsCycleFactorCycleSnapshot{
		{Start: statscyclefactorcontextCovDay(t, "2026-04-01"), FactorKeys: []string{models.CycleFactorStress}},
		{Start: statscyclefactorcontextCovDay(t, "2026-03-01"), FactorKeys: []string{models.CycleFactorTravel}},
		{Start: statscyclefactorcontextCovDay(t, "2026-02-01"), FactorKeys: []string{models.CycleFactorIllness}},
		{Start: statscyclefactorcontextCovDay(t, "2026-01-01"), FactorKeys: []string{models.CycleFactorSleepDisruption}},
	}

	summaries := buildStatsCycleFactorRecentCycles(snapshots)

	if len(summaries) != statsCycleFactorRecentCycleLimit {
		t.Errorf("expected truncation to %d recent cycles, got %d", statsCycleFactorRecentCycleLimit, len(summaries))
	}
	// Most-recent snapshot should be first.
	if !summaries[0].Start.Equal(statscyclefactorcontextCovDay(t, "2026-04-01")) {
		t.Errorf("expected most-recent cycle first, got %s", summaries[0].Start.Format("2006-01-02"))
	}
}

// ---------------------------------------------------------------------------
// Lines 233 + 235 – buildStatsCycleFactorItems sort order
// ---------------------------------------------------------------------------

// TestBuildStatsCycleFactorItemsSortsByCountDescending verifies that higher-
// count items appear before lower-count items (line 235 comparator).
//
// Survives mutant: inverting the > to < would reverse the sort.
func TestBuildStatsCycleFactorItemsSortsByCountDescending(t *testing.T) {
	counts := map[string]int{
		models.CycleFactorTravel: 3,
		models.CycleFactorStress: 1,
	}

	items := buildStatsCycleFactorItems(counts)

	if len(items) < 2 {
		t.Fatalf("expected at least two items, got %d", len(items))
	}
	if items[0].Key != models.CycleFactorTravel || items[0].Count != 3 {
		t.Errorf("expected travel (count=3) first, got key=%q count=%d", items[0].Key, items[0].Count)
	}
	if items[1].Key != models.CycleFactorStress || items[1].Count != 1 {
		t.Errorf("expected stress (count=1) second, got key=%q count=%d", items[1].Key, items[1].Count)
	}
}

// TestBuildStatsCycleFactorItemsTieBreaksByCanonicalOrder verifies that items
// with the same count are ordered by their position in supportedDayCycleFactorKeys
// (line 233 tie-breaker).
//
// Survives mutant: inverting < to > in the tie-breaker reverses canonical order.
func TestBuildStatsCycleFactorItemsTieBreaksByCanonicalOrder(t *testing.T) {
	// stress (index 0) and travel (index 2) both with count 1 – tie.
	counts := map[string]int{
		models.CycleFactorTravel: 1,
		models.CycleFactorStress: 1,
	}

	items := buildStatsCycleFactorItems(counts)

	if len(items) < 2 {
		t.Fatalf("expected at least two items, got %d", len(items))
	}
	// stress has canonical index 0, travel has canonical index 2 → stress first.
	if items[0].Key != models.CycleFactorStress {
		t.Errorf("expected stress (canonical index 0) before travel on tie, got %q first", items[0].Key)
	}
	if items[1].Key != models.CycleFactorTravel {
		t.Errorf("expected travel (canonical index 2) second on tie, got %q", items[1].Key)
	}
}

// ---------------------------------------------------------------------------
// Line 237 – context-item truncation at exactly the limit
// ---------------------------------------------------------------------------

// TestBuildStatsCycleFactorItemsExactlyAtContextLimit verifies that exactly
// statsCycleFactorContextLimit (3) items are all retained (check is >, not >=).
//
// Survives mutant: changing > to >= drops the third item when count==3.
func TestBuildStatsCycleFactorItemsExactlyAtContextLimit(t *testing.T) {
	counts := map[string]int{
		models.CycleFactorStress:  3,
		models.CycleFactorIllness: 2,
		models.CycleFactorTravel:  1,
	}

	items := buildStatsCycleFactorItems(counts)

	if len(items) != statsCycleFactorContextLimit {
		t.Errorf("expected %d items at exact limit, got %d", statsCycleFactorContextLimit, len(items))
	}
}

// TestBuildStatsCycleFactorItemsTruncatesAboveContextLimit verifies that more
// than statsCycleFactorContextLimit items are trimmed to the limit.
func TestBuildStatsCycleFactorItemsTruncatesAboveContextLimit(t *testing.T) {
	counts := map[string]int{
		models.CycleFactorStress:          4,
		models.CycleFactorIllness:         3,
		models.CycleFactorTravel:          2,
		models.CycleFactorSleepDisruption: 1,
	}

	items := buildStatsCycleFactorItems(counts)

	if len(items) != statsCycleFactorContextLimit {
		t.Errorf("expected truncation to %d items, got %d", statsCycleFactorContextLimit, len(items))
	}
}

// ---------------------------------------------------------------------------
// Line 246 – nonZeroStatsCycleFactorItems zero-count filter
// ---------------------------------------------------------------------------

// TestNonZeroStatsCycleFactorItemsExcludesZeroCounts verifies that keys with
// count=0 are excluded from the result (check is <=, which covers both 0 and
// negative; mutant changes <= to < which would include zero-count keys).
//
// Survives mutant: changing <= to < lets count=0 items through.
func TestNonZeroStatsCycleFactorItemsExcludesZeroCounts(t *testing.T) {
	counts := map[string]int{
		models.CycleFactorStress:  2,
		models.CycleFactorTravel:  0, // must be excluded
		models.CycleFactorIllness: 1,
	}

	items := nonZeroStatsCycleFactorItems(counts)

	for _, item := range items {
		if item.Count <= 0 {
			t.Errorf("expected no zero/negative-count items, got key=%q count=%d", item.Key, item.Count)
		}
		if item.Key == models.CycleFactorTravel {
			t.Errorf("expected travel (count=0) to be excluded, but it appeared in results")
		}
	}
	if len(items) != 2 {
		t.Errorf("expected exactly 2 non-zero items, got %d", len(items))
	}
}

// ---------------------------------------------------------------------------
// Lines 264–267 – hasStatsCycleFactorExplanation individual arms
// ---------------------------------------------------------------------------

// TestHasStatsCycleFactorExplanationRecentFactorsArm verifies that the function
// returns true when only RecentFactors is non-empty (line 264).
func TestHasStatsCycleFactorExplanationRecentFactorsArm(t *testing.T) {
	exp := StatsCycleFactorExplanation{
		RecentFactors: []StatsCycleFactorContextItem{{Key: models.CycleFactorStress, Count: 1}},
	}
	if !hasStatsCycleFactorExplanation(exp) {
		t.Error("expected true when only RecentFactors is set")
	}
}

// TestHasStatsCycleFactorExplanationPatternSummariesArm verifies line 265.
func TestHasStatsCycleFactorExplanationPatternSummariesArm(t *testing.T) {
	exp := StatsCycleFactorExplanation{
		PatternSummaries: []StatsCycleFactorPatternSummary{{Kind: "longer"}},
	}
	if !hasStatsCycleFactorExplanation(exp) {
		t.Error("expected true when only PatternSummaries is set")
	}
}

// TestHasStatsCycleFactorExplanationRecentCyclesArm verifies line 266.
func TestHasStatsCycleFactorExplanationRecentCyclesArm(t *testing.T) {
	exp := StatsCycleFactorExplanation{
		RecentCycles: []StatsCycleFactorRecentCycleSummary{{ComparisonKind: "shorter"}},
	}
	if !hasStatsCycleFactorExplanation(exp) {
		t.Error("expected true when only RecentCycles is set")
	}
}

// TestHasStatsCycleFactorExplanationHintFactorKeysArm verifies line 267.
func TestHasStatsCycleFactorExplanationHintFactorKeysArm(t *testing.T) {
	exp := StatsCycleFactorExplanation{
		HintFactorKeys: []string{models.CycleFactorStress},
	}
	if !hasStatsCycleFactorExplanation(exp) {
		t.Error("expected true when only HintFactorKeys is set")
	}
}

// TestHasStatsCycleFactorExplanationAllEmptyReturnsFalse verifies the false path.
func TestHasStatsCycleFactorExplanationAllEmptyReturnsFalse(t *testing.T) {
	if hasStatsCycleFactorExplanation(StatsCycleFactorExplanation{}) {
		t.Error("expected false for empty explanation")
	}
}
