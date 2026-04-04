package services

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubStatsDayReader struct {
	logsForRange   []models.DailyLog
	logsForAll     []models.DailyLog
	rangeErr       error
	allErr         error
	fetchAllCalled bool
	gotFrom        time.Time
	gotTo          time.Time
}

func (stub *stubStatsDayReader) FetchLogsForUser(_ uint, from time.Time, to time.Time, _ *time.Location) ([]models.DailyLog, error) {
	stub.gotFrom = from
	stub.gotTo = to
	if stub.rangeErr != nil {
		return nil, stub.rangeErr
	}
	result := make([]models.DailyLog, len(stub.logsForRange))
	copy(result, stub.logsForRange)
	return result, nil
}

func (stub *stubStatsDayReader) FetchAllLogsForUser(uint) ([]models.DailyLog, error) {
	stub.fetchAllCalled = true
	if stub.allErr != nil {
		return nil, stub.allErr
	}
	result := make([]models.DailyLog, len(stub.logsForAll))
	copy(result, stub.logsForAll)
	return result, nil
}

type stubStatsSymptomReader struct {
	frequencies []SymptomFrequency
	symptoms    []models.SymptomType
	err         error
}

func (stub *stubStatsSymptomReader) CalculateFrequencies(uint, []models.DailyLog) ([]SymptomFrequency, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]SymptomFrequency, len(stub.frequencies))
	copy(result, stub.frequencies)
	return result, nil
}

func (stub *stubStatsSymptomReader) FetchSymptoms(uint) ([]models.SymptomType, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]models.SymptomType, len(stub.symptoms))
	copy(result, stub.symptoms)
	return result, nil
}

func TestTrimTrailingCycleTrendLengths(t *testing.T) {
	source := []int{1, 2, 3, 4, 5}
	unchanged := TrimTrailingCycleTrendLengths(source, 10)
	if len(unchanged) != 5 || unchanged[0] != 1 || unchanged[4] != 5 {
		t.Fatalf("expected unchanged lengths, got %#v", unchanged)
	}

	trimmed := TrimTrailingCycleTrendLengths(source, 3)
	if len(trimmed) != 3 || trimmed[0] != 3 || trimmed[1] != 4 || trimmed[2] != 5 {
		t.Fatalf("expected trailing lengths [3 4 5], got %#v", trimmed)
	}
}

func TestOwnerBaselineCycleLength(t *testing.T) {
	tests := []struct {
		name string
		user *models.User
		want int
	}{
		{name: "nil user", user: nil, want: 0},
		{name: "unsupported role", user: &models.User{Role: "legacy_viewer", CycleLength: 29}, want: 0},
		{name: "owner invalid cycle length", user: &models.User{Role: models.RoleOwner, CycleLength: 120}, want: 0},
		{name: "owner valid cycle length", user: &models.User{Role: models.RoleOwner, CycleLength: 28}, want: 28},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := OwnerBaselineCycleLength(testCase.user); got != testCase.want {
				t.Fatalf("expected baseline %d, got %d", testCase.want, got)
			}
		})
	}
}

func TestBuildCycleStatsForRangeAppliesOwnerBaseline(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-02-10"), IsPeriod: true},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	userStart := mustParseStatsServiceDay(t, "2026-02-10")
	user := &models.User{
		ID:              7,
		Role:            models.RoleOwner,
		CycleLength:     29,
		PeriodLength:    6,
		LastPeriodStart: &userStart,
	}
	now := mustParseStatsServiceDay(t, "2026-02-20")

	stats, gotLogs, err := service.BuildCycleStatsForRange(user, now.AddDate(0, 0, -30), now, now, time.UTC)
	if err != nil {
		t.Fatalf("BuildCycleStatsForRange() unexpected error: %v", err)
	}
	if len(gotLogs) != 1 {
		t.Fatalf("expected one log entry, got %d", len(gotLogs))
	}
	if stats.MedianCycleLength != 29 {
		t.Fatalf("expected baseline median cycle length 29, got %d", stats.MedianCycleLength)
	}
}

func TestStatsOverviewRange(t *testing.T) {
	now := mustParseStatsServiceDay(t, "2026-03-02")
	from, to := StatsOverviewRange(now)

	if !to.Equal(now) {
		t.Fatalf("expected overview range to end at now, got %s", to)
	}
	expectedFrom := mustParseStatsServiceDay(t, "2024-03-02")
	if !from.Equal(expectedFrom) {
		t.Fatalf("expected overview range start %s, got %s", expectedFrom, from)
	}
}

func TestBuildOverviewStatsUsesOverviewRange(t *testing.T) {
	dayReader := &stubStatsDayReader{
		logsForRange: []models.DailyLog{
			{Date: mustParseStatsServiceDay(t, "2026-02-10"), IsPeriod: true},
		},
	}
	service := NewStatsService(dayReader, &stubStatsSymptomReader{})
	user := &models.User{ID: 3, Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-03-02")

	if _, err := service.BuildOverviewStats(user, now, time.UTC); err != nil {
		t.Fatalf("BuildOverviewStats() unexpected error: %v", err)
	}

	expectedFrom, expectedTo := StatsOverviewRange(now)
	if !dayReader.gotFrom.Equal(expectedFrom) {
		t.Fatalf("expected overview from %s, got %s", expectedFrom, dayReader.gotFrom)
	}
	if !dayReader.gotTo.Equal(expectedTo) {
		t.Fatalf("expected overview to %s, got %s", expectedTo, dayReader.gotTo)
	}
}

func TestBuildTrendAndFlags(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-03-26"), IsPeriod: true},
	}
	service := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-04-10")

	lengths, baseline := service.BuildTrend(user, logs, now, time.UTC, 2)
	if len(lengths) != 2 || lengths[0] != 28 || lengths[1] != 28 {
		t.Fatalf("expected trimmed trend lengths [28 28], got %#v", lengths)
	}
	if baseline != 28 {
		t.Fatalf("expected baseline 28, got %d", baseline)
	}

	stats := CycleStats{LastPeriodStart: mustParseStatsServiceDay(t, "2026-03-26")}
	flags := service.BuildFlags(user, logs, stats, now, time.UTC, len(lengths))
	if !flags.HasObservedCycleData || !flags.HasTrendData {
		t.Fatalf("expected observed and trend data flags true, got %#v", flags)
	}
	if !flags.HasInsights {
		t.Fatalf("expected HasInsights=true after two completed cycles")
	}
	if flags.CompletedCycleCount != 3 {
		t.Fatalf("expected CompletedCycleCount=3, got %d", flags.CompletedCycleCount)
	}
	if flags.InsightProgress != 100 {
		t.Fatalf("expected InsightProgress=100, got %d", flags.InsightProgress)
	}
	if flags.HasReliableTrend {
		t.Fatalf("expected HasReliableTrend=false for two trend points")
	}
}

func TestBuildFlagsKeepsInsightsLockedUntilTwoCompletedCycles(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
	}
	service := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-02-10")

	stats := CycleStats{
		LastPeriodStart:    mustParseStatsServiceDay(t, "2026-01-29"),
		AverageCycleLength: 28,
	}
	flags := service.BuildFlags(user, logs, stats, now, time.UTC, 1)

	if !flags.HasObservedCycleData {
		t.Fatalf("expected HasObservedCycleData=true")
	}
	if !flags.HasTrendData {
		t.Fatalf("expected HasTrendData=true")
	}
	if flags.HasInsights {
		t.Fatalf("expected HasInsights=false until two completed cycles")
	}
	if flags.CompletedCycleCount != 1 {
		t.Fatalf("expected CompletedCycleCount=1, got %d", flags.CompletedCycleCount)
	}
	if flags.InsightProgress != 50 {
		t.Fatalf("expected InsightProgress=50, got %d", flags.InsightProgress)
	}
	if flags.HasReliableTrend {
		t.Fatalf("expected HasReliableTrend=false for one trend point")
	}
}

func TestBuildSymptomFrequenciesForUserUnsupportedRoleSkipsDataAccess(t *testing.T) {
	dayReader := &stubStatsDayReader{}
	service := NewStatsService(dayReader, &stubStatsSymptomReader{})

	unsupported := &models.User{ID: 5, Role: "legacy_viewer"}
	frequencies, err := service.BuildSymptomFrequenciesForUser(unsupported)
	if err != nil {
		t.Fatalf("BuildSymptomFrequenciesForUser() unexpected error: %v", err)
	}
	if len(frequencies) != 0 {
		t.Fatalf("expected no frequencies for unsupported role, got %#v", frequencies)
	}
	if dayReader.fetchAllCalled {
		t.Fatalf("did not expect FetchAllLogsForUser call for unsupported role")
	}
}

func TestBuildSymptomFrequenciesForUserOwnerUsesLogsAndCalculator(t *testing.T) {
	dayReader := &stubStatsDayReader{logsForAll: []models.DailyLog{{ID: 1}}}
	expected := []SymptomFrequency{{Name: "Cramps", Count: 1, TotalDays: 1}}
	service := NewStatsService(dayReader, &stubStatsSymptomReader{frequencies: expected})

	owner := &models.User{ID: 8, Role: models.RoleOwner}
	frequencies, err := service.BuildSymptomFrequenciesForUser(owner)
	if err != nil {
		t.Fatalf("BuildSymptomFrequenciesForUser() unexpected error: %v", err)
	}
	if len(frequencies) != 1 || frequencies[0].Name != "Cramps" {
		t.Fatalf("expected one cramps frequency, got %#v", frequencies)
	}
	if !dayReader.fetchAllCalled {
		t.Fatalf("expected FetchAllLogsForUser call for owner")
	}
}

func TestBuildSymptomFrequenciesForUserPropagatesErrors(t *testing.T) {
	service := NewStatsService(&stubStatsDayReader{allErr: errors.New("load failed")}, &stubStatsSymptomReader{})
	owner := &models.User{ID: 9, Role: models.RoleOwner}

	if _, err := service.BuildSymptomFrequenciesForUser(owner); err == nil {
		t.Fatalf("expected error when logs loading fails")
	}
}

func mustParseStatsServiceDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}
