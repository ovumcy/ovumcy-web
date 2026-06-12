package services

// cycle_start_policy_coverage_test.go
//
// Kills surviving mutants in cycle_start_policy.go at lines:
//   28, 39        – nil-location guard in manualCycleStartMaxDate / IsAllowedManualCycleStartDate
//   48            – nil-location guard in ResolveManualCycleStartPolicy
//   61            – AddDate(0,0,-1) offset: anchor search must stop *before* targetDay
//   80            – filterLogsNotAfter(logs, targetDay.AddDate(0,0,-1)): targetDay log excluded from stats
//   81            – BuildCycleStats(filtered, targetDay.Add(-time.Second)): equivalent, no test needed
//   83, 86        – cycleLength <= 0 guards after predictedCycleLength / DashboardCycleReferenceLength:
//                   equivalent (both callees always return > 0), no test needed
//   103           – nil-location guard in LatestCycleStartAnchorBeforeOrOn

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// cyclestartpolicyCovDay parses "YYYY-MM-DD" into a UTC midnight time.Time.
func cyclestartpolicyCovDay(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		t.Fatalf("cyclestartpolicyCovDay: parse %q: %v", s, err)
	}
	return v
}

// ---------------------------------------------------------------------------
// Lines 28 + 39: nil location in manualCycleStartMaxDate / IsAllowedManualCycleStartDate
// ---------------------------------------------------------------------------

// TestCyclestartpolicyCov_IsAllowedManualCycleStartDate_NilLocation verifies
// that passing a nil *time.Location falls back to UTC and does not panic.
// The mutation that removes or inverts the nil-guard would cause a nil-pointer
// dereference in time.In(nil) before this function returns.
func TestCyclestartpolicyCov_IsAllowedManualCycleStartDate_NilLocation(t *testing.T) {
	// today and same day: must be allowed regardless of location nil-ness.
	now := cyclestartpolicyCovDay(t, "2026-04-15")
	day := cyclestartpolicyCovDay(t, "2026-04-15")

	// Explicit UTC result for reference.
	wantUTC := IsAllowedManualCycleStartDate(day, now, time.UTC)
	// Nil location must produce the same result (UTC semantics) without panicking.
	gotNil := IsAllowedManualCycleStartDate(day, now, nil)
	if gotNil != wantUTC {
		t.Fatalf("IsAllowedManualCycleStartDate with nil location = %v, want %v (same as UTC)", gotNil, wantUTC)
	}

	// A day 2 ahead is still allowed.
	dayPlus2 := cyclestartpolicyCovDay(t, "2026-04-17")
	if !IsAllowedManualCycleStartDate(dayPlus2, now, nil) {
		t.Fatal("expected today+2 to be allowed with nil location")
	}

	// A day 3 ahead is rejected.
	dayPlus3 := cyclestartpolicyCovDay(t, "2026-04-18")
	if IsAllowedManualCycleStartDate(dayPlus3, now, nil) {
		t.Fatal("expected today+3 to be rejected with nil location")
	}
}

// ---------------------------------------------------------------------------
// Line 48: nil location in ResolveManualCycleStartPolicy
// ---------------------------------------------------------------------------

// TestCyclestartpolicyCov_ResolveManualCycleStartPolicy_NilLocation verifies
// that nil location falls back to UTC. Without the nil-guard the function
// would panic when it calls DateAtLocation(day.In(nil), nil).
func TestCyclestartpolicyCov_ResolveManualCycleStartPolicy_NilLocation(t *testing.T) {
	prevStart := cyclestartpolicyCovDay(t, "2026-01-01")
	user := &models.User{}
	logs := []models.DailyLog{
		{Date: prevStart, IsPeriod: true, CycleStart: true},
	}
	now := cyclestartpolicyCovDay(t, "2026-02-15")
	day := cyclestartpolicyCovDay(t, "2026-01-10") // 9-day gap → short gap

	// Reference result with explicit UTC.
	pUTC := ResolveManualCycleStartPolicy(user, logs, day, now, time.UTC)
	// Nil must produce the same result without panicking.
	pNil := ResolveManualCycleStartPolicy(user, logs, day, now, nil)

	if pNil.ShortGapDays != pUTC.ShortGapDays {
		t.Fatalf("ShortGapDays: nil=%d UTC=%d", pNil.ShortGapDays, pUTC.ShortGapDays)
	}
	if !pNil.PreviousStart.Equal(pUTC.PreviousStart) {
		t.Fatalf("PreviousStart: nil=%v UTC=%v", pNil.PreviousStart, pUTC.PreviousStart)
	}
	// Short gap must be detected in both cases.
	if pNil.ShortGapDays == 0 {
		t.Fatal("expected ShortGapDays > 0 for a 9-day gap")
	}
}

// ---------------------------------------------------------------------------
// Line 103: nil location in LatestCycleStartAnchorBeforeOrOn
// ---------------------------------------------------------------------------

// TestCyclestartpolicyCov_LatestCycleStartAnchorBeforeOrOn_NilLocation verifies
// that nil location falls back to UTC and returns the expected anchor.
// The nil-guard is on line 103; removing it causes a panic inside DateAtLocation.
func TestCyclestartpolicyCov_LatestCycleStartAnchorBeforeOrOn_NilLocation(t *testing.T) {
	periodDay := cyclestartpolicyCovDay(t, "2026-03-01")
	user := &models.User{}
	logs := []models.DailyLog{
		{Date: periodDay, IsPeriod: true, CycleStart: true},
	}
	queryDay := cyclestartpolicyCovDay(t, "2026-03-15")

	anchorUTC := LatestCycleStartAnchorBeforeOrOn(user, logs, queryDay, time.UTC)
	anchorNil := LatestCycleStartAnchorBeforeOrOn(user, logs, queryDay, nil)

	if !anchorNil.Equal(anchorUTC) {
		t.Fatalf("LatestCycleStartAnchorBeforeOrOn nil location = %v, want %v", anchorNil, anchorUTC)
	}
	if anchorNil.IsZero() {
		t.Fatal("expected a non-zero anchor with nil location")
	}
}

// ---------------------------------------------------------------------------
// Line 61: AddDate(0,0,-1) — anchor search is strictly *before* targetDay
// ---------------------------------------------------------------------------

// TestCyclestartpolicyCov_ResolveManualCycleStartPolicy_AnchorOnTargetDayExcluded
// verifies that a cycle-start log on the exact targetDay is NOT used as the
// previousStart (the code passes targetDay.AddDate(0,0,-1) to
// LatestCycleStartAnchorBeforeOrOn). If the "-1" offset were removed (the
// surviving mutation), a same-day anchor would be returned, making gapDays==0
// and suppressing the short-gap flag unexpectedly for the *following* evaluation.
// Here we test the correct behaviour: when the only known anchor IS the
// target day, previousStart must be zero → no short-gap, no implantation flag.
func TestCyclestartpolicyCov_ResolveManualCycleStartPolicy_AnchorOnTargetDayExcluded(t *testing.T) {
	targetDay := cyclestartpolicyCovDay(t, "2026-03-15")
	user := &models.User{}
	// Only one log: a cycle start on the target day itself.
	logs := []models.DailyLog{
		{Date: targetDay, IsPeriod: true, CycleStart: true},
	}
	now := cyclestartpolicyCovDay(t, "2026-03-15")

	p := ResolveManualCycleStartPolicy(user, logs, targetDay, now, time.UTC)

	// Because the only anchor is ON the target day, no "previous" start exists.
	// ShortGapDays and PotentialImplantation must both be zero/false.
	if p.ShortGapDays != 0 {
		t.Fatalf("expected ShortGapDays=0 when only anchor is on target day, got %d", p.ShortGapDays)
	}
	if p.PotentialImplantation {
		t.Fatal("expected PotentialImplantation=false when only anchor is on target day")
	}
	if !p.PreviousStart.IsZero() {
		t.Fatalf("expected PreviousStart to be zero, got %v", p.PreviousStart)
	}
}

// TestCyclestartpolicyCov_ResolveManualCycleStartPolicy_AnchorDayBeforeTargetIncluded
// is the complementary case: an anchor one day before the target IS found,
// producing a short gap (gapDays = 1, which is > 0 and < 15).
func TestCyclestartpolicyCov_ResolveManualCycleStartPolicy_AnchorDayBeforeTargetIncluded(t *testing.T) {
	anchorDay := cyclestartpolicyCovDay(t, "2026-03-14")
	targetDay := cyclestartpolicyCovDay(t, "2026-03-15")
	user := &models.User{}
	logs := []models.DailyLog{
		{Date: anchorDay, IsPeriod: true, CycleStart: true},
	}
	now := cyclestartpolicyCovDay(t, "2026-03-15")

	p := ResolveManualCycleStartPolicy(user, logs, targetDay, now, time.UTC)

	// Anchor is 1 day before target → gapDays = 1 → short gap.
	if p.ShortGapDays != 1 {
		t.Fatalf("expected ShortGapDays=1, got %d", p.ShortGapDays)
	}
	if p.PreviousStart.IsZero() {
		t.Fatal("expected PreviousStart to be set when anchor is 1 day before target")
	}
}

// ---------------------------------------------------------------------------
// Line 80: filterLogsNotAfter(logs, targetDay.AddDate(0,0,-1)) —
//          a cycle-start log ON targetDay must not influence implantation stats.
// ---------------------------------------------------------------------------

// TestCyclestartpolicyCov_PotentialImplantationGapDays_TargetDayLogExcluded
// verifies that a CycleStart log dated on targetDay itself is excluded from the
// stats that feed potentialImplantationGapDays. The mutation would change the
// cutoff to targetDay (inclusive), potentially skewing the cycle-length stats.
//
// Setup: previousStart 2026-02-26, user CycleLength=28 (no prior logs).
// Ovulation predicted on 2026-03-11 (cycle day 14 of a 28-day cycle).
// targetDay = 2026-03-17 → gap = 6 → implantation window (lower edge).
//
// We add a synthetic "future" CycleStart on targetDay itself. If that log
// leaks into stats it would shorten the computed cycle length and shift the
// ovulation date, potentially making the result (6,true) differ.
func TestCyclestartpolicyCov_PotentialImplantationGapDays_TargetDayLogExcluded(t *testing.T) {
	user := &models.User{CycleLength: 28}
	previousStart := cyclestartpolicyCovDay(t, "2026-02-26")
	targetDay := cyclestartpolicyCovDay(t, "2026-03-17") // 6 days after ovulation

	// A log dated ON targetDay — must not leak into stats.
	logsWithTargetDayEntry := []models.DailyLog{
		{Date: targetDay, IsPeriod: true, CycleStart: true},
	}

	gap, ok := potentialImplantationGapDays(user, logsWithTargetDayEntry, targetDay, previousStart)
	if !ok {
		t.Fatal("expected potentialImplantationGapDays to return ok=true for 6-day gap")
	}
	if gap != 6 {
		t.Fatalf("expected gap=6, got %d (targetDay log may have leaked into stats)", gap)
	}

	// Confirm with no extra logs: result should be identical.
	gapClean, okClean := potentialImplantationGapDays(user, nil, targetDay, previousStart)
	if gap != gapClean || ok != okClean {
		t.Fatalf("result changed when targetDay log added: got (%d,%t) vs (%d,%t)", gap, ok, gapClean, okClean)
	}
}
