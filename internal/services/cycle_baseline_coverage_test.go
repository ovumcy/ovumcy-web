package services

// cycle_baseline_coverage_test.go
// Behavior tests targeting surviving mutants in cycle_baseline.go.
// All helpers/types are prefixed "cyclebaselineCov" to avoid collisions.

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// cyclebaselineCovOwner builds a minimal RoleOwner user with the supplied
// cycle settings. lastPeriod may be nil.
func cyclebaselineCovOwner(cycleLen, periodLen, luteal int, lastPeriod *time.Time) *models.User {
	return &models.User{
		Role:            models.RoleOwner,
		CycleLength:     cycleLen,
		PeriodLength:    periodLen,
		LutealPhase:     luteal,
		LastPeriodStart: lastPeriod,
	}
}

// cyclebaselineCovPeriodLog builds a simple period log entry at the given date.
func cyclebaselineCovPeriodLog(t *testing.T, date string) models.DailyLog {
	t.Helper()
	return models.DailyLog{
		Date:     mustParseBaselineDay(t, date),
		IsPeriod: true,
		Flow:     models.FlowMedium,
	}
}

// ── Line 13 ─────────────────────────────────────────────────────────────────
// ApplyUserCycleBaseline: nil location must not panic and must fall back to UTC.

func TestCycleBaseline_NilLocationFallbackInApply(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-01-10")
	user := cyclebaselineCovOwner(28, 5, 0, &lp)
	logs := []models.DailyLog{cyclebaselineCovPeriodLog(t, "2026-01-10")}
	now := mustParseBaselineDay(t, "2026-01-15")
	stats := BuildCycleStats(logs, now)
	// Must not panic and must return a non-zero LastPeriodStart.
	got := ApplyUserCycleBaseline(user, logs, stats, now, nil)
	if got.LastPeriodStart.IsZero() {
		t.Fatal("expected non-zero LastPeriodStart when location is nil")
	}
}

// ── Line 42 ─────────────────────────────────────────────────────────────────
// resolveUserCycleLengths: when PeriodLength is 0 (invalid), the default must
// be applied. If the mutation removes `periodLength = models.DefaultPeriodLength`
// the period length stays 0 and applyObservedBaseline writes 0 to
// stats.AveragePeriodLength.

func TestCycleBaseline_InvalidPeriodLengthDefaultsToModelDefault(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-03-01")
	// PeriodLength 0 is invalid per IsValidOnboardingPeriodLength
	user := cyclebaselineCovOwner(28, 0, 0, &lp)
	logs := []models.DailyLog{cyclebaselineCovPeriodLog(t, "2026-03-01")}
	now := mustParseBaselineDay(t, "2026-03-10")
	stats := BuildCycleStats(logs, now)
	got := ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)
	// AveragePeriodLength must be the model default, not 0.
	if got.AveragePeriodLength != float64(models.DefaultPeriodLength) {
		t.Fatalf("expected AveragePeriodLength=%d (default), got %.2f",
			models.DefaultPeriodLength, got.AveragePeriodLength)
	}
}

// ── Line 51 ─────────────────────────────────────────────────────────────────
// applyObservedBaseline: when there are no observed cycle lengths and cycleLength
// is valid, stats.AverageCycleLength and MedianCycleLength must be filled from
// the user setting. Zero cycleLength must NOT overwrite them.

func TestCycleBaseline_ZeroCycleLengthDoesNotOverwriteStatsWhenNoHistory(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-03-01")
	// CycleLength 0 is not valid — must not fill stats.
	user := cyclebaselineCovOwner(0, 5, 0, &lp)
	logs := []models.DailyLog{cyclebaselineCovPeriodLog(t, "2026-03-01")}
	now := mustParseBaselineDay(t, "2026-03-10")
	stats := BuildCycleStats(logs, now)
	got := ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)
	// With no observed cycles and cycleLength=0, both must stay 0.
	if got.AverageCycleLength != 0 {
		t.Fatalf("expected AverageCycleLength=0 for zero user cycleLength, got %.2f", got.AverageCycleLength)
	}
	if got.MedianCycleLength != 0 {
		t.Fatalf("expected MedianCycleLength=0 for zero user cycleLength, got %d", got.MedianCycleLength)
	}
}

func TestCycleBaseline_ValidCycleLengthFillsStatsWhenNoHistory(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-03-01")
	user := cyclebaselineCovOwner(30, 5, 0, &lp)
	logs := []models.DailyLog{cyclebaselineCovPeriodLog(t, "2026-03-01")}
	now := mustParseBaselineDay(t, "2026-03-10")
	stats := BuildCycleStats(logs, now)
	got := ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)
	if got.AverageCycleLength != 30 {
		t.Fatalf("expected AverageCycleLength=30 from user setting, got %.2f", got.AverageCycleLength)
	}
	if got.MedianCycleLength != 30 {
		t.Fatalf("expected MedianCycleLength=30 from user setting, got %d", got.MedianCycleLength)
	}
}

// ── Line 55 ─────────────────────────────────────────────────────────────────
// applyObservedBaseline: AveragePeriodLength must be filled from the user's
// period length when there are no observed cycle lengths and period length > 0.

func TestCycleBaseline_ValidPeriodLengthFillsAveragePeriodLengthWhenNoHistory(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-03-01")
	user := cyclebaselineCovOwner(28, 7, 0, &lp)
	logs := []models.DailyLog{cyclebaselineCovPeriodLog(t, "2026-03-01")}
	now := mustParseBaselineDay(t, "2026-03-10")
	stats := BuildCycleStats(logs, now)
	got := ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)
	if got.AveragePeriodLength != 7 {
		t.Fatalf("expected AveragePeriodLength=7 from user setting, got %.2f", got.AveragePeriodLength)
	}
}

// ── Lines 75 & 78 ────────────────────────────────────────────────────────────
// applyProjectedBaseline lines 75–78:
// predictionCycleLength comes from predictedCycleLength(median, average) which
// always returns at least models.DefaultCycleLength — so predictionCycleLength
// is never ≤ 0 in practice. Lines 75 and 78 are unreachable defensive guards
// (equivalent mutants). We keep a smoke test confirming that projection runs
// normally and produces a valid NextPeriodStart for a typical no-history user.

func TestCycleBaseline_ProjectionProducesNextPeriodStartForNoHistoryUser(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-03-01")
	user := cyclebaselineCovOwner(28, 5, 0, &lp)
	logs := []models.DailyLog{cyclebaselineCovPeriodLog(t, "2026-03-01")}
	now := mustParseBaselineDay(t, "2026-03-10")
	stats := BuildCycleStats(logs, now)
	got := ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)
	// With no observed cycle lengths, predictedCycleLength uses the user's
	// cycleLength=28 (set via line 51 guard) and produces a NextPeriodStart.
	if got.NextPeriodStart.IsZero() {
		t.Fatal("expected non-zero NextPeriodStart for no-history user with cycleLength=28")
	}
	want := "2026-03-29"
	if gotStr := got.NextPeriodStart.Format("2006-01-02"); gotStr != want {
		t.Fatalf("expected NextPeriodStart=%s, got %s", want, gotStr)
	}
}

// ── Line 117 ─────────────────────────────────────────────────────────────────
// DetectCurrentPhase: nil location must not panic and must fall back to UTC.

func TestCycleBaseline_DetectCurrentPhaseNilLocationFallback(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-03-01")
	user := cyclebaselineCovOwner(28, 5, 0, &lp)
	logs := []models.DailyLog{cyclebaselineCovPeriodLog(t, "2026-03-01")}
	now := mustParseBaselineDay(t, "2026-03-10")
	statsBase := BuildCycleStats(logs, now)
	statsBase = ApplyUserCycleBaseline(user, logs, statsBase, now, time.UTC)
	// Calling DetectCurrentPhase with nil location must not panic.
	got := DetectCurrentPhase(statsBase, logs, now, nil)
	if got == "" {
		t.Fatal("DetectCurrentPhase returned empty string with nil location")
	}
}

// ── Line 130 ─────────────────────────────────────────────────────────────────
// DetectCurrentPhase rounding: AveragePeriodLength=4.6 must round to 5 (not 4).
// A today on the 5th day of period must return "menstrual"; if truncated (=4),
// day 5 would fall outside the window and return a different phase.

func TestCycleBaseline_DetectCurrentPhaseRoundsAveragePeriodLengthUp(t *testing.T) {
	lastPeriodStart := mustParseBaselineDay(t, "2026-04-01")
	// Craft stats with AveragePeriodLength=4.6 (rounds to 5) and known OvulationDate.
	stats := CycleStats{
		LastPeriodStart:      lastPeriodStart,
		AveragePeriodLength:  4.6,
		OvulationDate:        mustParseBaselineDay(t, "2026-04-15"),
		OvulationExact:       true,
		FertilityWindowStart: mustParseBaselineDay(t, "2026-04-10"),
		FertilityWindowEnd:   mustParseBaselineDay(t, "2026-04-15"),
	}
	// today = day 5 of the cycle (2026-04-05), which is inside period if rounded to 5.
	today := mustParseBaselineDay(t, "2026-04-05")
	got := DetectCurrentPhase(stats, nil, today, time.UTC)
	if got != "menstrual" {
		t.Fatalf("expected 'menstrual' on day 5 with AveragePeriodLength=4.6 (rounds to 5), got %q", got)
	}
}

// ── Line 131 ─────────────────────────────────────────────────────────────────
// DetectCurrentPhase: when AveragePeriodLength rounds to 0, the default must
// be applied so that today on day 1 is still detected as "menstrual".

func TestCycleBaseline_DetectCurrentPhaseZeroPeriodLengthUsesDefault(t *testing.T) {
	lastPeriodStart := mustParseBaselineDay(t, "2026-04-01")
	stats := CycleStats{
		LastPeriodStart:      lastPeriodStart,
		AveragePeriodLength:  0, // would produce periodLength=0 before default fix
		OvulationDate:        mustParseBaselineDay(t, "2026-04-15"),
		OvulationExact:       true,
		FertilityWindowStart: mustParseBaselineDay(t, "2026-04-10"),
		FertilityWindowEnd:   mustParseBaselineDay(t, "2026-04-15"),
	}
	// today = day 1 of period. With periodLength defaulted to models.DefaultPeriodLength
	// it should be within the menstrual window.
	today := lastPeriodStart
	got := DetectCurrentPhase(stats, nil, today, time.UTC)
	if got != "menstrual" {
		t.Fatalf("expected 'menstrual' on first day when AveragePeriodLength=0 (default applied), got %q", got)
	}
}

// ── Line 135 ─────────────────────────────────────────────────────────────────
// DetectCurrentPhase: period end is LastPeriodStart + (periodLength-1) days.
// Day `periodLength` must be OUTSIDE the menstrual window (one day too late).

func TestCycleBaseline_DetectCurrentPhaseLastDayOfPeriodIsInclusive(t *testing.T) {
	lastPeriodStart := mustParseBaselineDay(t, "2026-04-01")
	stats := CycleStats{
		LastPeriodStart:      lastPeriodStart,
		AveragePeriodLength:  5,
		OvulationDate:        mustParseBaselineDay(t, "2026-04-15"),
		OvulationExact:       true,
		FertilityWindowStart: mustParseBaselineDay(t, "2026-04-10"),
		FertilityWindowEnd:   mustParseBaselineDay(t, "2026-04-15"),
	}
	// periodLength=5 → periodEnd = April 5 (day 5, 0-indexed +4 from April 1).
	// April 5 should be "menstrual"; April 6 should NOT be.
	lastDay := mustParseBaselineDay(t, "2026-04-05")
	if got := DetectCurrentPhase(stats, nil, lastDay, time.UTC); got != "menstrual" {
		t.Fatalf("day 5 (last day of 5-day period) should be 'menstrual', got %q", got)
	}
	dayAfter := mustParseBaselineDay(t, "2026-04-06")
	if got := DetectCurrentPhase(stats, nil, dayAfter, time.UTC); got == "menstrual" {
		t.Fatalf("day 6 (after 5-day period) should NOT be 'menstrual', got %q", got)
	}
}

// ── Line 162 ─────────────────────────────────────────────────────────────────
// ProjectCycleStart: zero lastPeriodStart or non-positive cycleLength must
// return (zero, 0, false).

func TestCycleBaseline_ProjectCycleStartRejectsZeroLastPeriodStart(t *testing.T) {
	today := mustParseBaselineDay(t, "2026-04-10")
	start, day, ok := ProjectCycleStart(time.Time{}, 28, today)
	if ok {
		t.Fatalf("expected ok=false for zero lastPeriodStart, got start=%v day=%d", start, day)
	}
}

func TestCycleBaseline_ProjectCycleStartRejectsZeroCycleLength(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-04-01")
	today := mustParseBaselineDay(t, "2026-04-10")
	start, day, ok := ProjectCycleStart(lp, 0, today)
	if ok {
		t.Fatalf("expected ok=false for cycleLength=0, got start=%v day=%d", start, day)
	}
}

func TestCycleBaseline_ProjectCycleStartRejectsNegativeCycleLength(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-04-01")
	today := mustParseBaselineDay(t, "2026-04-10")
	start, day, ok := ProjectCycleStart(lp, -1, today)
	if ok {
		t.Fatalf("expected ok=false for negative cycleLength, got start=%v day=%d", start, day)
	}
}

// ── Line 172 ─────────────────────────────────────────────────────────────────
// ProjectCycleStart: projectedCycleDay must be 1-based (first day of a new
// cycle has cycleDay=1, not 0).

func TestCycleBaseline_ProjectCycleStartReturnsCycleDay1OnFirstDay(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-04-01")
	// today == lastPeriodStart → elapsedDays=0, cycleDay should be 1.
	start, day, ok := ProjectCycleStart(lp, 28, lp)
	if !ok {
		t.Fatal("expected ok=true when today==lastPeriodStart")
	}
	if day != 1 {
		t.Fatalf("expected cycleDay=1 on the first day of the cycle, got %d", day)
	}
	if !start.Equal(lp) {
		t.Fatalf("expected projectedStart=%v, got %v", lp, start)
	}
}

func TestCycleBaseline_ProjectCycleStartReturnsCycleDay1AtCycleBoundary(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-04-01")
	// After exactly one full cycle (28 days), we're on the first day of the next cycle.
	today := mustParseBaselineDay(t, "2026-04-29") // lp + 28 days
	start, day, ok := ProjectCycleStart(lp, 28, today)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if day != 1 {
		t.Fatalf("expected cycleDay=1 at the start of a new cycle, got %d", day)
	}
	wantStart := mustParseBaselineDay(t, "2026-04-29")
	if !start.Equal(wantStart) {
		t.Fatalf("expected projectedStart=%v, got %v", wantStart, start)
	}
}

func TestCycleBaseline_ProjectCycleStartMidCycleDayIsCorrect(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-04-01")
	// Day 15 of cycle (14 days elapsed) → cycleDay should be 15.
	today := mustParseBaselineDay(t, "2026-04-15") // lp + 14 days
	_, day, ok := ProjectCycleStart(lp, 28, today)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if day != 15 {
		t.Fatalf("expected cycleDay=15, got %d", day)
	}
}

// ── Line 177 ─────────────────────────────────────────────────────────────────
// ShiftCycleStartToFutureOvulation: must NOT shift when ovulationDate is in
// the future (>= today), and must shift when ovulationDate is strictly in the
// past.

func TestCycleBaseline_ShiftCycleStartNoShiftWhenOvulationIsFuture(t *testing.T) {
	today := mustParseBaselineDay(t, "2026-04-10")
	cycleStart := mustParseBaselineDay(t, "2026-04-01")
	ovulationFuture := mustParseBaselineDay(t, "2026-04-15") // after today
	got := ShiftCycleStartToFutureOvulation(cycleStart, ovulationFuture, 28, today)
	if !got.Equal(cycleStart) {
		t.Fatalf("expected cycleStart unchanged when ovulation is in the future, got %v", got)
	}
}

func TestCycleBaseline_ShiftCycleStartNoShiftWhenOvulationIsToday(t *testing.T) {
	today := mustParseBaselineDay(t, "2026-04-10")
	cycleStart := mustParseBaselineDay(t, "2026-04-01")
	// ovulationDate == today → not strictly before today → no shift.
	got := ShiftCycleStartToFutureOvulation(cycleStart, today, 28, today)
	if !got.Equal(cycleStart) {
		t.Fatalf("expected cycleStart unchanged when ovulation == today, got %v", got)
	}
}

func TestCycleBaseline_ShiftCycleStartShiftsWhenOvulationIsPast(t *testing.T) {
	today := mustParseBaselineDay(t, "2026-04-20")
	cycleStart := mustParseBaselineDay(t, "2026-04-01")
	ovulationPast := mustParseBaselineDay(t, "2026-04-10") // before today
	got := ShiftCycleStartToFutureOvulation(cycleStart, ovulationPast, 28, today)
	if got.Equal(cycleStart) {
		t.Fatalf("expected cycleStart to be shifted when ovulation is past, but got same value %v", got)
	}
	// Shifted start must be strictly after cycleStart.
	if !got.After(cycleStart) {
		t.Fatalf("expected shifted cycleStart to be after original %v, got %v", cycleStart, got)
	}
}

func TestCycleBaseline_ShiftCycleStartNoShiftWhenCycleLengthIsZero(t *testing.T) {
	today := mustParseBaselineDay(t, "2026-04-20")
	cycleStart := mustParseBaselineDay(t, "2026-04-01")
	ovulationPast := mustParseBaselineDay(t, "2026-04-10")
	got := ShiftCycleStartToFutureOvulation(cycleStart, ovulationPast, 0, today)
	if !got.Equal(cycleStart) {
		t.Fatalf("expected cycleStart unchanged for cycleLength=0, got %v", got)
	}
}
