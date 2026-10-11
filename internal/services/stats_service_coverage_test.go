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

// TestStatsServiceBuildOverviewStatsPropagatesToStorage verifies that when
// the underlying FetchLogsForUser fails, BuildOverviewStats returns the same
// error (not nil, not a different error), so callers can inspect the cause.
func TestStatsServiceBuildOverviewStatsPropagatesToStorage(t *testing.T) {
	svc := NewStatsService(&statsserviceCovFailDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{ID: 1, Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-05-01")

	_, _, err := svc.BuildOverviewStats(context.Background(), user, now, time.UTC)
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

// TestStatsServiceTrimTrailingCycleTrendLengthsZeroMaxPoints verifies that a
// maxPoints of 0 (non-positive) returns the original slice unchanged, not an
// empty slice.  A mutant that drops the "maxPoints <= 0" guard would return
// lengths[len(lengths)-0:] = lengths[len(lengths):] = empty.
func TestStatsServiceTrimTrailingCycleTrendLengthsZeroMaxPoints(t *testing.T) {
	input := []int{10, 20, 30}
	got := TrimTrailingCycleTrendLengths(input, 0)
	if len(got) != 3 {
		t.Fatalf("expected unchanged slice for maxPoints=0, got %v", got)
	}
	if got[0] != 10 || got[2] != 30 {
		t.Fatalf("expected original values [10 20 30], got %v", got)
	}
}

// TestStatsServiceTrimTrailingCycleTrendLengthsNegativeMaxPoints mirrors the
// zero case for negative values.
func TestStatsServiceTrimTrailingCycleTrendLengthsNegativeMaxPoints(t *testing.T) {
	input := []int{5, 6}
	got := TrimTrailingCycleTrendLengths(input, -1)
	if len(got) != 2 {
		t.Fatalf("expected unchanged slice for maxPoints=-1, got %v", got)
	}
}

// TestStatsServiceTrimTrailingCycleTrendLengthsExactEquality verifies that
// when len(lengths) == maxPoints the slice is returned as-is (no trim).
// A mutant that changes "<=" to "<" would incorrectly trim the slice.
func TestStatsServiceTrimTrailingCycleTrendLengthsExactEquality(t *testing.T) {
	input := []int{11, 22, 33}
	got := TrimTrailingCycleTrendLengths(input, 3) // len == maxPoints
	if len(got) != 3 || got[0] != 11 || got[2] != 33 {
		t.Fatalf("expected unchanged slice when len==maxPoints, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// BuildFlags field-boundary conditions
// ---------------------------------------------------------------------------

// statsserviceCovEmptyLogs is a shared empty log slice for flag boundary tests.
var statsserviceCovEmptyLogs []models.DailyLog

// statsserviceCovFlags derives the flags the way the stats page does: the
// cycle stats come out of the logs, and the flags read the count off them.
func statsserviceCovFlags(t *testing.T, logs []models.DailyLog, now time.Time, trendPointCount int) StatsFlags {
	t.Helper()
	svc := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	return svc.BuildFlags(user, BuildCycleStatsFromLogs(user, logs, now, time.UTC), trendPointCount)
}

// TestStatsServiceBuildFlagsHasObservedCycleDataFalseAtZero verifies that
// HasObservedCycleData is false when there are no logs at all.
// A mutant changing "> 0" to ">= 0" would set this true even with zero cycles.
func TestStatsServiceBuildFlagsHasObservedCycleDataFalseAtZero(t *testing.T) {
	flags := statsserviceCovFlags(t, statsserviceCovEmptyLogs, mustParseStatsServiceDay(t, "2026-05-01"), 0)

	if flags.HasObservedCycleData {
		t.Fatal("expected HasObservedCycleData=false for empty logs, got true")
	}
}

// TestStatsServiceBuildFlagsHasTrendDataFalseAtZero verifies that HasTrendData
// is false when trendPointCount == 0.
// A mutant changing "> 0" to ">= 0" would set this true.
func TestStatsServiceBuildFlagsHasTrendDataFalseAtZero(t *testing.T) {
	// A completed cycle exists, but we pass trendPointCount=0.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true},
	}
	flags := statsserviceCovFlags(t, logs, mustParseStatsServiceDay(t, "2026-05-01"), 0)

	if flags.HasTrendData {
		t.Fatal("expected HasTrendData=false for trendPointCount=0, got true")
	}
	if !flags.HasObservedCycleData {
		t.Fatal("expected HasObservedCycleData=true for one completed cycle, got false")
	}
}

// TestStatsServiceBuildFlagsHasInsightsTrueAtExactMinimum verifies that
// HasInsights is true when the completed-cycle count equals
// statsMinimumInsightsCycles (which is 3).  A mutant changing ">=" to ">" would
// set this false.
func TestStatsServiceBuildFlagsHasInsightsTrueAtExactMinimum(t *testing.T) {
	// Four period-start logs produce exactly 3 completed cycles.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true, CycleStart: true},
		{Date: mustParseStatsServiceDay(t, "2026-03-26"), IsPeriod: true, CycleStart: true},
	}
	flags := statsserviceCovFlags(t, logs, mustParseStatsServiceDay(t, "2026-04-10"), 3)

	if !flags.HasInsights {
		t.Fatalf("expected HasInsights=true when completedCycleCount==statsMinimumInsightsCycles(3), got false; completedCycleCount=%d", flags.CompletedCycleCount)
	}
	if flags.InsightCyclesRequired != 3 {
		t.Fatalf("expected InsightCyclesRequired=3, got %d", flags.InsightCyclesRequired)
	}
}

// TestStatsServiceBuildFlagsHasReliableTrendTrueAtExactMinimum verifies that
// HasReliableTrend is true when trendPointCount equals statsReliableTrendCycles
// (which is 3).  A mutant changing ">=" to ">" would set this false.
func TestStatsServiceBuildFlagsHasReliableTrendTrueAtExactMinimum(t *testing.T) {
	flags := statsserviceCovFlags(t, statsserviceCovEmptyLogs, mustParseStatsServiceDay(t, "2026-05-01"), 3 /* trendPointCount == statsReliableTrendCycles */)

	if !flags.HasReliableTrend {
		t.Fatal("expected HasReliableTrend=true when trendPointCount==statsReliableTrendCycles(3), got false")
	}
}

// TestStatsServiceBuildFlagsCountsWhatTheStatsCount pins the one counter: the
// flags take the completed-cycle count from the CycleStats they are handed, the
// counter the dashboard's fertility floor reads, and from nothing else.
func TestStatsServiceBuildFlagsCountsWhatTheStatsCount(t *testing.T) {
	svc := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}

	for _, count := range []int{0, 2, 3, 5} {
		flags := svc.BuildFlags(user, CycleStats{CompletedCycleCount: count}, 0)
		if flags.CompletedCycleCount != count {
			t.Errorf("CompletedCycleCount = %d, want %d", flags.CompletedCycleCount, count)
		}
		if want := count >= fertilityMinimumCycles; flags.HasInsights != want {
			t.Errorf("HasInsights at %d completed cycles = %v, want %v", count, flags.HasInsights, want)
		}
		if want := count > 0; flags.HasObservedCycleData != want {
			t.Errorf("HasObservedCycleData at %d completed cycles = %v, want %v", count, flags.HasObservedCycleData, want)
		}
	}
}
