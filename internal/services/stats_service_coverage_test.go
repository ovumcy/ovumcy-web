package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// errStatsserviceCovSentinel is a unique error used to distinguish propagation
// from any coincidental nil/zero return.
var errStatsserviceCovSentinel = errors.New("statsserviceCov: sentinel fetch error")

// statsserviceCovFailDayReader always returns the sentinel error from
// FetchLogsForUser, simulating a storage failure path.
type statsserviceCovFailDayReader struct{}

func (r *statsserviceCovFailDayReader) FetchLogsForUser(ctx context.Context, _ uint, _, _ time.Time, _ *time.Location) ([]models.DailyLog, error) {
	return nil, errStatsserviceCovSentinel
}

func (r *statsserviceCovFailDayReader) FetchAllLogsForUser(ctx context.Context, _ uint) ([]models.DailyLog, error) {
	return nil, errStatsserviceCovSentinel
}

// ---------------------------------------------------------------------------
// Line 66 — BuildOverviewStats error path
// ---------------------------------------------------------------------------

// TestStatsserviceCovBuildOverviewStatsPropagatesToStorage verifies that when
// the underlying FetchLogsForUser fails, BuildOverviewStats returns the same
// error (not nil, not a different error), so callers can inspect the cause.
func TestStatsserviceCovBuildOverviewStatsPropagatesToStorage(t *testing.T) {
	svc := NewStatsService(&statsserviceCovFailDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{ID: 1, Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-05-01")

	_, err := svc.BuildOverviewStats(context.Background(), user, now, time.UTC)
	if err == nil {
		t.Fatal("BuildOverviewStats() expected error, got nil")
	}
	if !errors.Is(err, errStatsserviceCovSentinel) {
		t.Fatalf("BuildOverviewStats() expected sentinel error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Line 73 — TrimTrailingCycleTrendLengths boundary conditions
// ---------------------------------------------------------------------------

// TestStatsserviceCovTrimTrailingCycleTrendLengthsZeroMaxPoints verifies that a
// maxPoints of 0 (non-positive) returns the original slice unchanged, not an
// empty slice.  A mutant that drops the "maxPoints <= 0" guard would return
// lengths[len(lengths)-0:] = lengths[len(lengths):] = empty.
func TestStatsserviceCovTrimTrailingCycleTrendLengthsZeroMaxPoints(t *testing.T) {
	input := []int{10, 20, 30}
	got := TrimTrailingCycleTrendLengths(input, 0)
	if len(got) != 3 {
		t.Fatalf("expected unchanged slice for maxPoints=0, got %v", got)
	}
	if got[0] != 10 || got[2] != 30 {
		t.Fatalf("expected original values [10 20 30], got %v", got)
	}
}

// TestStatsserviceCovTrimTrailingCycleTrendLengthsNegativeMaxPoints mirrors the
// zero case for negative values.
func TestStatsserviceCovTrimTrailingCycleTrendLengthsNegativeMaxPoints(t *testing.T) {
	input := []int{5, 6}
	got := TrimTrailingCycleTrendLengths(input, -1)
	if len(got) != 2 {
		t.Fatalf("expected unchanged slice for maxPoints=-1, got %v", got)
	}
}

// TestStatsserviceCovTrimTrailingCycleTrendLengthsExactEquality verifies that
// when len(lengths) == maxPoints the slice is returned as-is (no trim).
// A mutant that changes "<=" to "<" would incorrectly trim the slice.
func TestStatsserviceCovTrimTrailingCycleTrendLengthsExactEquality(t *testing.T) {
	input := []int{11, 22, 33}
	got := TrimTrailingCycleTrendLengths(input, 3) // len == maxPoints
	if len(got) != 3 || got[0] != 11 || got[2] != 33 {
		t.Fatalf("expected unchanged slice when len==maxPoints, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Lines 103-106 — BuildFlags field-boundary conditions
// ---------------------------------------------------------------------------

// statsserviceCovEmptyLogs is a shared empty log slice for flag boundary tests.
var statsserviceCovEmptyLogs []models.DailyLog

// TestStatsserviceCovBuildFlagsHasObservedCycleDataFalseAtZero verifies that
// HasObservedCycleData is false when there are no logs at all.
// A mutant changing "> 0" to ">= 0" would set this true even with zero cycles.
func TestStatsserviceCovBuildFlagsHasObservedCycleDataFalseAtZero(t *testing.T) {
	svc := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-05-01")
	flags := svc.BuildFlags(user, statsserviceCovEmptyLogs, CycleStats{}, now, time.UTC, 0)

	if flags.HasObservedCycleData {
		t.Fatal("expected HasObservedCycleData=false for empty logs, got true")
	}
}

// TestStatsserviceCovBuildFlagsHasTrendDataFalseAtZero verifies that HasTrendData
// is false when trendPointCount == 0.
// A mutant changing "> 0" to ">= 0" on line 104 would set this true.
func TestStatsserviceCovBuildFlagsHasTrendDataFalseAtZero(t *testing.T) {
	svc := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-05-01")
	// Two-period logs ensure observedCycleCount > 0, but we pass trendPointCount=0.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
	}
	flags := svc.BuildFlags(user, logs, CycleStats{}, now, time.UTC, 0)

	if flags.HasTrendData {
		t.Fatal("expected HasTrendData=false for trendPointCount=0, got true")
	}
}

// TestStatsserviceCovBuildFlagsHasInsightsTrueAtExactMinimum verifies that
// HasInsights is true when completedCycleCount equals statsMinimumInsightsCycles
// (which is 2).  A mutant changing ">=" to ">" on line 105 would set this false.
func TestStatsserviceCovBuildFlagsHasInsightsTrueAtExactMinimum(t *testing.T) {
	svc := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-04-10")

	// Three period-start logs produce exactly 2 completed cycles.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
	}
	flags := svc.BuildFlags(user, logs, CycleStats{}, now, time.UTC, 2)

	if !flags.HasInsights {
		t.Fatalf("expected HasInsights=true when completedCycleCount==statsMinimumInsightsCycles(2), got false; completedCycleCount=%d", flags.CompletedCycleCount)
	}
}

// TestStatsserviceCovBuildFlagsHasReliableTrendTrueAtExactMinimum verifies that
// HasReliableTrend is true when trendPointCount equals statsReliableTrendCycles
// (which is 3).  A mutant changing ">=" to ">" on line 106 would set this false.
func TestStatsserviceCovBuildFlagsHasReliableTrendTrueAtExactMinimum(t *testing.T) {
	svc := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-05-01")

	flags := svc.BuildFlags(user, statsserviceCovEmptyLogs, CycleStats{}, now, time.UTC, 3 /* trendPointCount == statsReliableTrendCycles */)

	if !flags.HasReliableTrend {
		t.Fatal("expected HasReliableTrend=true when trendPointCount==statsReliableTrendCycles(3), got false")
	}
}

// ---------------------------------------------------------------------------
// Lines 114 & 119 — statsInsightProgress boundary conditions
// ---------------------------------------------------------------------------

// TestStatsserviceCovStatsInsightProgressZeroCompletedCycles verifies that
// InsightProgress is 0 when completedCycleCount is 0.
// A mutant changing "<= 0" to "< 0" on line 114 would skip the guard and
// produce 0*100/2 = 0 anyway — but the concrete branch is still untested,
// so we confirm the return value AND that completedCycleCount=0 produces 0.
func TestStatsserviceCovStatsInsightProgressZeroCompletedCycles(t *testing.T) {
	svc := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-05-01")

	// Empty logs → completedCycleCount = 0.
	flags := svc.BuildFlags(user, statsserviceCovEmptyLogs, CycleStats{}, now, time.UTC, 0)

	if flags.InsightProgress != 0 {
		t.Fatalf("expected InsightProgress=0 for zero completed cycles, got %d", flags.InsightProgress)
	}
	if flags.CompletedCycleCount != 0 {
		t.Fatalf("expected CompletedCycleCount=0 for empty logs, got %d", flags.CompletedCycleCount)
	}
}

// TestStatsserviceCovStatsInsightProgressAtExactHundred verifies that when
// progress equals 100 exactly, the function returns 100 and does NOT fall into
// the "progress > 100 → 100" branch.  A mutant changing "> 100" to ">= 100" on
// line 119 would incorrectly force the return-100 path even when progress==100,
// which is fine numerically but tests a different code path; the real concern is
// the one-below edge: progress=99 must NOT be capped to 100.
func TestStatsserviceCovStatsInsightProgressAtExactHundred(t *testing.T) {
	// statsMinimumInsightsCycles = 2; completedCycleCount = 2 → progress = 2*100/2 = 100.
	// Should return 100 (not capped, just equal).
	svc := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-04-10")

	// Three period logs → exactly 2 completed cycles.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
	}
	flags := svc.BuildFlags(user, logs, CycleStats{}, now, time.UTC, 2)

	if flags.InsightProgress != 100 {
		t.Fatalf("expected InsightProgress=100 for completedCycleCount==statsMinimumInsightsCycles, got %d", flags.InsightProgress)
	}
}

// TestStatsserviceCovStatsInsightProgressBelowHundredNotCapped verifies that a
// progress value below 100 is returned as-is without being capped.
// Concretely completedCycleCount=1, statsMinimumInsightsCycles=2: 1*100/2=50.
// If the "progress > 100" guard were changed to "> 0", 50 would be capped to 100.
func TestStatsserviceCovStatsInsightProgressBelowHundredNotCapped(t *testing.T) {
	svc := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-03-01")

	// Two period logs → exactly 1 completed cycle.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
	}
	flags := svc.BuildFlags(user, logs, CycleStats{}, now, time.UTC, 1)

	if flags.InsightProgress != 50 {
		t.Fatalf("expected InsightProgress=50 for one completed cycle, got %d", flags.InsightProgress)
	}
}
