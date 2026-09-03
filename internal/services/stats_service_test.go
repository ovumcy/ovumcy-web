package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The stats doubles observe the owner operand of every read they serve. All
// four reads are owner-scoped in production — the ranged logs, all logs, the
// frequency calculation and the symptom catalogue — and while the doubles
// discarded the id, each of those call sites could be re-pointed at a constant
// owner with the whole stats selection still green. The *ByOwner maps let a
// test serve different data per owner so the rendered page, not just the
// recorded id, tells the two apart.
type stubStatsDayReader struct {
	logsForRange   []models.DailyLog
	logsForAll     []models.DailyLog
	logsByOwner    map[uint][]models.DailyLog
	rangeErr       error
	allErr         error
	fetchAllCalled bool
	gotFrom        time.Time
	gotTo          time.Time
	gotRangeOwner  uint
	gotAllOwner    uint
}

func (stub *stubStatsDayReader) FetchLogsForUser(ctx context.Context, userID uint, from time.Time, to time.Time, _ *time.Location) ([]models.DailyLog, error) {
	stub.gotRangeOwner = userID
	stub.gotFrom = from
	stub.gotTo = to
	if stub.rangeErr != nil {
		return nil, stub.rangeErr
	}
	return copyDailyLogsForOwner(stub.logsByOwner, userID, stub.logsForRange), nil
}

func (stub *stubStatsDayReader) FetchAllLogsForUser(_ context.Context, userID uint) ([]models.DailyLog, error) {
	stub.gotAllOwner = userID
	stub.fetchAllCalled = true
	if stub.allErr != nil {
		return nil, stub.allErr
	}
	return copyDailyLogsForOwner(stub.logsByOwner, userID, stub.logsForAll), nil
}

// copyDailyLogsForOwner serves the owner's own rows when the test supplied a
// per-owner map, and the flat slice otherwise. It always copies: a double that
// hands out its own backing array lets the subject mutate the fixture.
func copyDailyLogsForOwner(byOwner map[uint][]models.DailyLog, userID uint, fallback []models.DailyLog) []models.DailyLog {
	source := fallback
	if byOwner != nil {
		source = byOwner[userID]
	}
	result := make([]models.DailyLog, len(source))
	copy(result, source)
	return result
}

type stubStatsSymptomReader struct {
	frequencies        []SymptomFrequency
	frequenciesByOwner map[uint][]SymptomFrequency
	symptoms           []models.SymptomType
	symptomsByOwner    map[uint][]models.SymptomType
	err                error
	gotFrequencyOwner  uint
	gotSymptomOwner    uint
}

func (stub *stubStatsSymptomReader) CalculateFrequencies(_ context.Context, userID uint, _ []models.DailyLog) ([]SymptomFrequency, error) {
	stub.gotFrequencyOwner = userID
	if stub.err != nil {
		return nil, stub.err
	}
	source := stub.frequencies
	if stub.frequenciesByOwner != nil {
		source = stub.frequenciesByOwner[userID]
	}
	result := make([]SymptomFrequency, len(source))
	copy(result, source)
	return result, nil
}

func (stub *stubStatsSymptomReader) FetchSymptoms(_ context.Context, userID uint) ([]models.SymptomType, error) {
	stub.gotSymptomOwner = userID
	if stub.err != nil {
		return nil, stub.err
	}
	source := stub.symptoms
	if stub.symptomsByOwner != nil {
		source = stub.symptomsByOwner[userID]
	}
	result := make([]models.SymptomType, len(source))
	copy(result, source)
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

	stats, gotLogs, err := service.BuildCycleStatsForRange(context.Background(), user, now.AddDate(0, 0, -30), now, now, time.UTC)
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

func TestBuildCycleStatsForRangePausesAfterPositivePregnancyTest(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-02-10"), PregnancyTest: models.PregnancyTestPositive},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	user := &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-02-20")

	stats, _, err := service.BuildCycleStatsForRange(context.Background(), user, now.AddDate(0, 0, -30), now, now, time.UTC)
	if err != nil {
		t.Fatalf("BuildCycleStatsForRange() unexpected error: %v", err)
	}
	if !stats.PregnancyPaused {
		t.Fatal("expected a positive pregnancy test with no later cycle start to pause predictions")
	}
}

func TestBuildCycleStatsForRangeResumesWhenCycleStartFollowsPositiveTest(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-02-10"), PregnancyTest: models.PregnancyTestPositive},
		{Date: mustParseStatsServiceDay(t, "2026-02-18"), IsPeriod: true, CycleStart: true},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	user := &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-02-20")

	stats, _, err := service.BuildCycleStatsForRange(context.Background(), user, now.AddDate(0, 0, -30), now, now, time.UTC)
	if err != nil {
		t.Fatalf("BuildCycleStatsForRange() unexpected error: %v", err)
	}
	if stats.PregnancyPaused {
		t.Fatal("expected a cycle start after the positive test to resume predictions")
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

	if _, _, err := service.BuildOverviewStats(context.Background(), user, now, time.UTC); err != nil {
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
	frequencies, err := service.BuildSymptomFrequenciesForUser(context.Background(), unsupported)
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
	frequencies, err := service.BuildSymptomFrequenciesForUser(context.Background(), owner)
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

	if _, err := service.BuildSymptomFrequenciesForUser(context.Background(), owner); err == nil {
		t.Fatalf("expected error when logs loading fails")
	}
}

// TestStatsServiceBuildCycleStatsFromLogsIsThePackageFunction pins that the
// method the dashboard's interface seam depends on adds nothing to the
// package-level derivation. The repository-free callers (the webhook decision
// pass, the .ics feed) now call the function directly; a method that started
// computing something of its own would silently give the dashboard a different
// prediction from the two egress surfaces.
//
// It also pins that the function needs no service at all — it is called here on
// a receiver-less path, which is the "consults no repositories" property the
// egress callers used to assert in a comment and buy with NewStatsService(nil,
// nil).
func TestStatsServiceBuildCycleStatsFromLogsIsThePackageFunction(t *testing.T) {
	// The fixture drives all three steps of the derivation, so agreeing on it is
	// not the same as agreeing on raw BuildCycleStats: the owner's non-default
	// luteal phase only reaches the stats through ApplyUserCycleBaseline, and the
	// pregnancy flag only through ResolvePregnancyPause.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true, CycleStart: true},
		{Date: mustParseStatsServiceDay(t, "2026-03-08"), PregnancyTest: models.PregnancyTestPositive},
	}
	user := &models.User{ID: 9, Role: models.RoleOwner, CycleLength: 28, LutealPhase: 11}
	now := mustParseStatsServiceDay(t, "2026-03-10")

	direct := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	raw := BuildCycleStats(logs, now)
	if direct == raw {
		t.Fatalf("anchor: the fixture must exercise the baseline and pause steps, got the raw derivation %+v", raw)
	}
	if !direct.PregnancyPaused || direct.LutealPhase != 11 {
		t.Fatalf("anchor: expected the pause flag and the owner's luteal phase, got %+v", direct)
	}

	viaMethod := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{}).BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if viaMethod != direct {
		t.Fatalf("method result %+v differs from the package function's %+v", viaMethod, direct)
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
