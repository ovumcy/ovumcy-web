package services

// dashboard_cycle_coverage_test.go — behavior tests for the dashboard cycle
// summary logic in dashboard_cycle.go: reference-length fallbacks, long-cycle
// and stale-data detection, prediction ranges, and next-period/ovulation data
// gating. Written to kill surviving mutants (gremlins); every test asserts
// observable behavior (return values, computed dates, boolean flags).

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ---------------------------------------------------------------------------
// Line 61 — DashboardCycleReferenceLength: fallback to user.CycleLength
//   when stats have no data and user != nil and cycle length is valid.
// A mutant could remove the user != nil guard or change to user == nil;
// we also verify that a nil user still returns DefaultCycleLength.
// ---------------------------------------------------------------------------

func TestDashboardCycleReferenceLengthFallsBackToUserCycleLength(t *testing.T) {
	user := &models.User{CycleLength: 32}
	stats := CycleStats{} // no AverageCycleLength, no MedianCycleLength

	got := DashboardCycleReferenceLength(user, stats)
	if got != 32 {
		t.Fatalf("expected user.CycleLength=32 as fallback, got %d", got)
	}
}

func TestDashboardCycleReferenceLengthNilUserFallsBackToDefault(t *testing.T) {
	// When user is nil and stats are empty, the function must NOT panic and
	// must return DefaultCycleLength.
	got := DashboardCycleReferenceLength(nil, CycleStats{})
	if got != models.DefaultCycleLength {
		t.Fatalf("expected DefaultCycleLength=%d for nil user with no stats, got %d", models.DefaultCycleLength, got)
	}
}

func TestDashboardCycleReferenceLengthInvalidUserCycleUsesDefault(t *testing.T) {
	// An invalid onboarding cycle length (e.g. 0 or 200) should be rejected
	// and DefaultCycleLength returned.
	user := &models.User{CycleLength: 0}
	got := DashboardCycleReferenceLength(user, CycleStats{})
	if got != models.DefaultCycleLength {
		t.Fatalf("expected DefaultCycleLength for invalid user cycle 0, got %d", got)
	}

	user2 := &models.User{CycleLength: 200}
	got2 := DashboardCycleReferenceLength(user2, CycleStats{})
	if got2 != models.DefaultCycleLength {
		t.Fatalf("expected DefaultCycleLength for invalid user cycle 200, got %d", got2)
	}
}

// ---------------------------------------------------------------------------
// Line 68 — DashboardCycleDayLooksLong: guard conditions (currentDay <= 0
// or referenceLength <= 0 must return false)
// ---------------------------------------------------------------------------

func TestDashboardCycleCycleDayLooksLongZeroDayReturnsFalse(t *testing.T) {
	if DashboardCycleDayLooksLong(0, 28) {
		t.Fatal("expected false for currentDay=0")
	}
}

func TestDashboardCycleCycleDayLooksLongNegativeDayReturnsFalse(t *testing.T) {
	if DashboardCycleDayLooksLong(-1, 28) {
		t.Fatal("expected false for currentDay=-1")
	}
}

func TestDashboardCycleCycleDayLooksLongZeroReferenceReturnsFalse(t *testing.T) {
	if DashboardCycleDayLooksLong(40, 0) {
		t.Fatal("expected false when referenceLength=0")
	}
}

// ---------------------------------------------------------------------------
// Line 71 — DashboardCycleDayLooksLong: the +7 threshold
// A day exactly at referenceLength+7 must NOT trigger; referenceLength+8 must.
// ---------------------------------------------------------------------------

func TestDashboardCycleCycleDayLooksLongBoundaryAtPlusSeven(t *testing.T) {
	const ref = 28
	// exactly at the boundary: not long
	if DashboardCycleDayLooksLong(ref+7, ref) {
		t.Fatalf("day=%d with ref=%d: expected false (boundary is exclusive), got true", ref+7, ref)
	}
	// one past: long
	if !DashboardCycleDayLooksLong(ref+8, ref) {
		t.Fatalf("day=%d with ref=%d: expected true (over threshold), got false", ref+8, ref)
	}
}

// ---------------------------------------------------------------------------
// Line 75 — DashboardCycleDataLooksStale: guard conditions
//   - zero lastPeriodStart returns false
//   - referenceLength <= 0 returns false
//   - today before lastPeriodStart returns false
// ---------------------------------------------------------------------------

func TestDashboardCycleDataLooksStaleZeroAnchorReturnsFalse(t *testing.T) {
	today := mustParseDashboardDay(t, "2026-04-10")
	if DashboardCycleDataLooksStale(time.Time{}, today, 28) {
		t.Fatal("expected false for zero lastPeriodStart")
	}
}

func TestDashboardCycleDataLooksStaleZeroReferenceReturnsFalse(t *testing.T) {
	anchor := mustParseDashboardDay(t, "2026-03-01")
	today := mustParseDashboardDay(t, "2026-04-10")
	if DashboardCycleDataLooksStale(anchor, today, 0) {
		t.Fatal("expected false for referenceLength=0")
	}
}

func TestDashboardCycleDataLooksStaleTodayBeforeAnchorReturnsFalse(t *testing.T) {
	anchor := mustParseDashboardDay(t, "2026-04-10")
	today := mustParseDashboardDay(t, "2026-03-01")
	if DashboardCycleDataLooksStale(anchor, today, 28) {
		t.Fatal("expected false when today is before lastPeriodStart")
	}
}

// ---------------------------------------------------------------------------
// Line 78 — DashboardCycleDataLooksStale: the +1 in rawCycleDay
// On the exact same day as lastPeriodStart, rawCycleDay = 1.
// For a reference length of 1, that is NOT stale (1 > 1 is false).
// ---------------------------------------------------------------------------

func TestDashboardCycleDataLooksStaleFirstDayNotStale(t *testing.T) {
	anchor := mustParseDashboardDay(t, "2026-04-01")
	today := mustParseDashboardDay(t, "2026-04-01") // same day, cycle day 1
	// ref=1: cycle day (1) > ref (1) is false => not stale
	if DashboardCycleDataLooksStale(anchor, today, 1) {
		t.Fatal("expected not stale on cycle day 1 with ref=1")
	}
	// ref=28: cycle day 1 > 28 is false => not stale
	if DashboardCycleDataLooksStale(anchor, today, 28) {
		t.Fatal("expected not stale on day 0 elapsed with ref=28")
	}
}

// ---------------------------------------------------------------------------
// Line 79 — DashboardCycleDataLooksStale: rawCycleDay > referenceLength
// Exactly at ref: not stale. One beyond: stale.
// ---------------------------------------------------------------------------

func TestDashboardCycleDataLooksStaleBoundary(t *testing.T) {
	anchor := mustParseDashboardDay(t, "2026-03-01")
	const ref = 28

	// rawCycleDay = 28 (27 days elapsed + 1) — not stale
	dayAtRef := anchor.AddDate(0, 0, ref-1)
	if DashboardCycleDataLooksStale(anchor, dayAtRef, ref) {
		t.Fatalf("day at exactly ref=%d should not be stale", ref)
	}

	// rawCycleDay = 29 — stale
	dayPastRef := anchor.AddDate(0, 0, ref)
	if !DashboardCycleDataLooksStale(anchor, dayPastRef, ref) {
		t.Fatalf("day at ref+1=%d should be stale", ref+1)
	}
}

// ---------------------------------------------------------------------------
// Line 86 — DashboardCycleStaleAnchor: when stats is zero, fall through to user.
// Mutations targeting user==nil check, user.LastPeriodStart==nil, or IsZero().
// ---------------------------------------------------------------------------

func TestDashboardCycleStaleAnchorNilUserWithEmptyStatsReturnsZero(t *testing.T) {
	anchor := DashboardCycleStaleAnchor(nil, CycleStats{}, time.UTC)
	if !anchor.IsZero() {
		t.Fatalf("expected zero time for nil user and empty stats, got %v", anchor)
	}
}

func TestDashboardCycleStaleAnchorNonNilUserNilLastPeriodStartReturnsZero(t *testing.T) {
	user := &models.User{LastPeriodStart: nil}
	anchor := DashboardCycleStaleAnchor(user, CycleStats{}, time.UTC)
	if !anchor.IsZero() {
		t.Fatalf("expected zero time when user.LastPeriodStart is nil, got %v", anchor)
	}
}

func TestDashboardCycleStaleAnchorUserLastPeriodStartUsedWhenStatsEmpty(t *testing.T) {
	lps := mustParseDashboardDay(t, "2026-02-15")
	user := &models.User{LastPeriodStart: &lps}
	anchor := DashboardCycleStaleAnchor(user, CycleStats{}, time.UTC)
	if got := anchor.Format("2006-01-02"); got != "2026-02-15" {
		t.Fatalf("expected 2026-02-15 from user.LastPeriodStart, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// Line 102 — dashboardPredictionRegularSpan: CompletedCycleCount < 3 returns 0
// Line 102 — CycleLengthStdDev <= 0 also returns 0
// ---------------------------------------------------------------------------

func TestDashboardCyclePredictionRangeNeedsAtLeastThreeCycles(t *testing.T) {
	// Two completed cycles: no range
	rangeStart, rangeEnd, ok := DashboardPredictionRange(
		&models.User{},
		CycleStats{CompletedCycleCount: 2, CycleLengthStdDev: 3.0},
		mustParseDashboardDay(t, "2026-04-07"),
		time.UTC,
	)
	if ok {
		t.Fatalf("expected no range for CompletedCycleCount=2, got start=%v end=%v", rangeStart, rangeEnd)
	}
}

func TestDashboardCyclePredictionRangeThreeCyclesWithZeroStdDevNoRange(t *testing.T) {
	// Three completed cycles but stddev=0: still no range
	_, _, ok := DashboardPredictionRange(
		&models.User{},
		CycleStats{CompletedCycleCount: 3, CycleLengthStdDev: 0},
		mustParseDashboardDay(t, "2026-04-07"),
		time.UTC,
	)
	if ok {
		t.Fatal("expected no range for CycleLengthStdDev=0 even with 3 cycles")
	}
}

// ---------------------------------------------------------------------------
// Line 106 — dashboardPredictionRegularSpan: span < 1 is clamped to 1
// StdDev = 0.4 rounds to 0, which is < 1, so span becomes 1.
// The test in dashboard_cycle_test.go already exercises this via
// DashboardPredictionRange ("low variability rounds up to one day").
// Confirm directly on span boundary: 3 cycles, stddev = 0.3 (rounds to 0 -> 1).
// ---------------------------------------------------------------------------

func TestDashboardCycleRegularSpanClampedToOneForVeryLowStdDev(t *testing.T) {
	predictedStart := mustParseDashboardDay(t, "2026-04-07")
	rangeStart, rangeEnd, ok := DashboardPredictionRange(
		&models.User{},
		CycleStats{CompletedCycleCount: 3, CycleLengthStdDev: 0.3},
		predictedStart,
		time.UTC,
	)
	if !ok {
		t.Fatal("expected a range when CompletedCycleCount=3 and StdDev=0.3")
	}
	startISO := rangeStart.Format("2006-01-02")
	endISO := rangeEnd.Format("2006-01-02")
	// span should be clamped to 1: start = predictedStart - 1, end = predictedStart + 1
	if startISO != "2026-04-06" || endISO != "2026-04-08" {
		t.Fatalf("expected ±1 day range [2026-04-06, 2026-04-08], got [%s, %s]", startISO, endISO)
	}
}

// ---------------------------------------------------------------------------
// Line 109 — dashboardPredictionRegularSpan: span > 5 is clamped to 5
// StdDev = 10 rounds to 10, which is > 5, so span becomes 5.
// (already tested for stddev=8.7 in existing file, but we add a boundary test)
// ---------------------------------------------------------------------------

func TestDashboardCycleRegularSpanClampedToFiveForVeryHighStdDev(t *testing.T) {
	predictedStart := mustParseDashboardDay(t, "2026-04-07")
	rangeStart, rangeEnd, ok := DashboardPredictionRange(
		&models.User{},
		CycleStats{CompletedCycleCount: 10, CycleLengthStdDev: 10.0},
		predictedStart,
		time.UTC,
	)
	if !ok {
		t.Fatal("expected a range when StdDev=10 and CompletedCycleCount=10")
	}
	startISO := rangeStart.Format("2006-01-02")
	endISO := rangeEnd.Format("2006-01-02")
	// span must be exactly 5 (clamped from 10)
	if startISO != "2026-04-02" || endISO != "2026-04-12" {
		t.Fatalf("expected ±5 day range [2026-04-02, 2026-04-12], got [%s, %s]", startISO, endISO)
	}
}

// ---------------------------------------------------------------------------
// Line 116 — dashboardIrregularPredictionRangeEnabled: all conjuncts required.
// A mutant removing any single condition must be caught.
// ---------------------------------------------------------------------------

func TestDashboardCycleIrregularRangeRequiresIrregularFlag(t *testing.T) {
	// user.IrregularCycle = false → should NOT use irregular range
	user := &models.User{IrregularCycle: false}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		CompletedCycleCount: 5,
		MinCycleLength:      24,
		MaxCycleLength:      36,
		AverageCycleLength:  30,
	}
	ctx := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if ctx.DisplayNextPeriodUseRange && ctx.DisplayOvulationUseRange {
		t.Fatal("expected no irregular range when IrregularCycle=false")
	}
}

func TestDashboardCycleIrregularRangeRequiresThreeCompletedCycles(t *testing.T) {
	// CompletedCycleCount < 3 → should NOT use irregular range even with flag set
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		CompletedCycleCount: 2,
		MinCycleLength:      24,
		MaxCycleLength:      36,
		AverageCycleLength:  30,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-04-01"),
	}
	ctx := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if ctx.DisplayNextPeriodUseRange {
		t.Fatal("expected no irregular prediction range with CompletedCycleCount=2")
	}
}

func TestDashboardCycleIrregularRangeRequiresPositiveMinLength(t *testing.T) {
	// MinCycleLength = 0 → should NOT use irregular range
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		CompletedCycleCount: 5,
		MinCycleLength:      0,
		MaxCycleLength:      36,
		AverageCycleLength:  30,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-04-01"),
	}
	ctx := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if ctx.DisplayNextPeriodUseRange {
		t.Fatal("expected no irregular range when MinCycleLength=0")
	}
}

func TestDashboardCycleIrregularRangeRequiresMaxGeMin(t *testing.T) {
	// MaxCycleLength < MinCycleLength → should NOT use irregular range
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		CompletedCycleCount: 5,
		MinCycleLength:      30,
		MaxCycleLength:      25, // invalid: max < min
		AverageCycleLength:  28,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-04-01"),
	}
	ctx := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if ctx.DisplayNextPeriodUseRange {
		t.Fatal("expected no irregular range when MaxCycleLength < MinCycleLength")
	}
}

// ---------------------------------------------------------------------------
// Line 160 — DashboardUpcomingPredictions: guard for zero LastPeriodStart or
// cycleLength <= 0 returns raw stats values unchanged.
// ---------------------------------------------------------------------------

func TestDashboardCycleUpcomingPredictionsZeroLastPeriodStartPassesThroughStats(t *testing.T) {
	statsPeriod := mustParseDashboardDay(t, "2026-05-01")
	statsOv := mustParseDashboardDay(t, "2026-04-17")
	stats := CycleStats{
		LastPeriodStart:     time.Time{}, // zero
		NextPeriodStart:     statsPeriod,
		OvulationDate:       statsOv,
		OvulationExact:      true,
		OvulationImpossible: false,
	}
	np, ov, exact, impossible := DashboardUpcomingPredictions(stats, &models.User{}, mustParseDashboardDay(t, "2026-04-10"), 28)
	if !np.Equal(statsPeriod) {
		t.Fatalf("expected pass-through NextPeriodStart=%v, got %v", statsPeriod, np)
	}
	if !ov.Equal(statsOv) {
		t.Fatalf("expected pass-through OvulationDate=%v, got %v", statsOv, ov)
	}
	if !exact {
		t.Fatal("expected pass-through OvulationExact=true")
	}
	if impossible {
		t.Fatal("expected pass-through OvulationImpossible=false")
	}
}

func TestDashboardCycleUpcomingPredictionsZeroCycleLengthPassesThroughStats(t *testing.T) {
	statsPeriod := mustParseDashboardDay(t, "2026-05-01")
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-04-01"),
		NextPeriodStart:     statsPeriod,
		OvulationExact:      false,
		OvulationImpossible: true,
	}
	np, _, _, impossible := DashboardUpcomingPredictions(stats, &models.User{}, mustParseDashboardDay(t, "2026-04-10"), 0)
	if !np.Equal(statsPeriod) {
		t.Fatalf("expected pass-through NextPeriodStart for cycleLength=0, got %v", np)
	}
	if !impossible {
		t.Fatal("expected pass-through OvulationImpossible=true for cycleLength=0")
	}
}

// ---------------------------------------------------------------------------
// Line 254 — dashboardNeedsNextPeriodData: all conjuncts matter.
// Exercised via BuildDashboardCycleContext since the function is unexported.
// ---------------------------------------------------------------------------

func TestDashboardCycleNeedsNextPeriodDataRequiresIrregularFlag(t *testing.T) {
	// IrregularCycle=false → DisplayNextPeriodNeedsData must be false
	user := &models.User{IrregularCycle: false}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		CompletedCycleCount: 1,
		AverageCycleLength:  28,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-03-29"),
	}
	ctx := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-10"), time.UTC)
	if ctx.DisplayNextPeriodNeedsData {
		t.Fatal("expected DisplayNextPeriodNeedsData=false when IrregularCycle=false")
	}
}

func TestDashboardCycleNeedsNextPeriodDataRequiresFewCycles(t *testing.T) {
	// CompletedCycleCount >= 3 with IrregularCycle → needs-data flag must be false
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		CompletedCycleCount: 3,
		MinCycleLength:      24,
		MaxCycleLength:      36,
		AverageCycleLength:  30,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-04-01"),
	}
	ctx := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if ctx.DisplayNextPeriodNeedsData {
		t.Fatal("expected DisplayNextPeriodNeedsData=false when CompletedCycleCount=3")
	}
}

func TestDashboardCycleNeedsNextPeriodDataRequiresNonZeroNextPeriod(t *testing.T) {
	// IrregularCycle=true, <3 cycles, but no period data at all → nextPeriodStart
	// remains zero (DashboardUpcomingPredictions passes through stats.NextPeriodStart
	// which is also zero), so needs-data must be false.
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:     time.Time{}, // zero — forces pass-through in DashboardUpcomingPredictions
		CompletedCycleCount: 1,
		AverageCycleLength:  28,
		NextPeriodStart:     time.Time{}, // also zero
	}
	ctx := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-10"), time.UTC)
	if ctx.DisplayNextPeriodNeedsData {
		t.Fatal("expected DisplayNextPeriodNeedsData=false when nextPeriodStart resolves to zero")
	}
}

// ---------------------------------------------------------------------------
// Line 299 — dashboardNextPeriodEnd: guard periodLength <= 0 returns zero.
// When AveragePeriodLength rounds to 0 and DefaultPeriodLength takes effect,
// verify the end date is non-zero.
// When nextPeriodStart is zero, verify a zero end date.
// ---------------------------------------------------------------------------

func TestDashboardCycleNextPeriodEndZeroStartReturnsZero(t *testing.T) {
	end := dashboardNextPeriodEnd(time.Time{}, CycleStats{AveragePeriodLength: 5}, time.UTC)
	if !end.IsZero() {
		t.Fatalf("expected zero end for zero start, got %v", end)
	}
}

func TestDashboardCycleNextPeriodEndZeroAveragePeriodUsesDefault(t *testing.T) {
	// AveragePeriodLength=0 should not return zero time; DefaultPeriodLength kicks in.
	start := mustParseDashboardDay(t, "2026-04-07")
	end := dashboardNextPeriodEnd(start, CycleStats{AveragePeriodLength: 0}, time.UTC)
	if end.IsZero() {
		t.Fatal("expected non-zero end when AveragePeriodLength=0 (DefaultPeriodLength should kick in)")
	}
	// DefaultPeriodLength = 5, so end = start + (5-1) days = 2026-04-11
	if got := end.Format("2006-01-02"); got != "2026-04-11" {
		t.Fatalf("expected 2026-04-11 with default period length, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// Line 324 — dashboardNeedsOvulationData: all conjuncts matter.
// Exercised via BuildDashboardCycleContext.
// ---------------------------------------------------------------------------

func TestDashboardCycleNeedsOvulationDataRequiresIrregularFlag(t *testing.T) {
	user := &models.User{IrregularCycle: false}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		CompletedCycleCount: 1,
		AverageCycleLength:  28,
	}
	ctx := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-10"), time.UTC)
	if ctx.DisplayOvulationNeedsData {
		t.Fatal("expected DisplayOvulationNeedsData=false when IrregularCycle=false")
	}
}

func TestDashboardCycleNeedsOvulationDataRequiresFewCycles(t *testing.T) {
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		CompletedCycleCount: 3,
		MinCycleLength:      24,
		MaxCycleLength:      36,
		AverageCycleLength:  30,
	}
	ctx := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if ctx.DisplayOvulationNeedsData {
		t.Fatal("expected DisplayOvulationNeedsData=false when CompletedCycleCount=3")
	}
}

func TestDashboardCycleNeedsOvulationDataRequiresNonZeroLastPeriodStart(t *testing.T) {
	// IrregularCycle=true, <3 cycles, but LastPeriodStart is zero → needs-data must be false
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:     time.Time{}, // zero
		CompletedCycleCount: 1,
		AverageCycleLength:  28,
	}
	ctx := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-10"), time.UTC)
	if ctx.DisplayOvulationNeedsData {
		t.Fatal("expected DisplayOvulationNeedsData=false when LastPeriodStart is zero")
	}
}
