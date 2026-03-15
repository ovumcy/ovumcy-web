package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestBuildStatsPageViewDataOwnerBuildsTrendBaselineAndSymptomSummaries(t *testing.T) {
	dayReader := &stubStatsDayReader{
		logsForRange: []models.DailyLog{
			{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
			{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
			{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
			{Date: mustParseStatsServiceDay(t, "2026-03-26"), IsPeriod: true},
		},
		logsForAll: []models.DailyLog{{ID: 1}},
	}
	service := NewStatsService(dayReader, &stubStatsSymptomReader{
		frequencies: []SymptomFrequency{
			{Name: "Headache", Icon: "H", Count: 1, TotalDays: 1},
		},
	})

	user := &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-04-10")

	viewData, err := service.BuildStatsPageViewData(user, "en", "Cycle %d", now, time.UTC, 2)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}

	assertOwnerTrendViewData(t, viewData)
}

func TestBuildStatsPageViewDataIrregularNoticeRespectsUserMode(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-25"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-03-10"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-04-20"), IsPeriod: true},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := mustParseStatsServiceDay(t, "2026-04-25")

	regularUser := &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 32}
	regularView, err := service.BuildStatsPageViewData(regularUser, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error for regular user: %v", err)
	}
	if !regularView.ShowIrregularityNotice {
		t.Fatalf("expected irregularity notice for spread greater than seven days")
	}
	if regularView.IsIrregularMode {
		t.Fatalf("expected IsIrregularMode=false for regular user")
	}
	if regularView.ChartBaseline != 36 {
		t.Fatalf("expected averaged chart baseline 36, got %d", regularView.ChartBaseline)
	}

	irregularUser := &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 32, IrregularCycle: true}
	irregularView, err := service.BuildStatsPageViewData(irregularUser, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error for irregular user: %v", err)
	}
	if irregularView.ShowIrregularityNotice {
		t.Fatalf("expected irregularity notice to be suppressed in irregular mode")
	}
	if !irregularView.IsIrregularMode {
		t.Fatalf("expected IsIrregularMode=true for irregular user")
	}
}

func TestBuildStatsPageViewDataShowsIrregularInsufficientDataNotice(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := mustParseStatsServiceDay(t, "2026-02-10")

	viewData, err := service.BuildStatsPageViewData(&models.User{ID: 8, Role: models.RoleOwner, IrregularCycle: true}, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if !viewData.ShowIrregularInsufficientDataNotice {
		t.Fatalf("expected ShowIrregularInsufficientDataNotice=true")
	}
}

func TestBuildStatsPageViewDataBuildsRecentCycleFactorContextForVariablePatterns(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-03"), CycleFactorKeys: []string{models.CycleFactorStress}},
		{Date: mustParseStatsServiceDay(t, "2026-01-25"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-28"), CycleFactorKeys: []string{models.CycleFactorTravel}},
		{Date: mustParseStatsServiceDay(t, "2026-03-10"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-03-12"), CycleFactorKeys: []string{models.CycleFactorStress}},
		{Date: mustParseStatsServiceDay(t, "2026-04-20"), IsPeriod: true},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs, logsForAll: logs}, &stubStatsSymptomReader{})
	now := mustParseStatsServiceDay(t, "2026-04-25")

	viewData, err := service.BuildStatsPageViewData(&models.User{ID: 7, Role: models.RoleOwner, CycleLength: 32}, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if !viewData.HasRecentCycleFactors {
		t.Fatalf("expected recent cycle factor context")
	}
	if len(viewData.RecentCycleFactors) != 2 {
		t.Fatalf("expected two recent factor items, got %#v", viewData.RecentCycleFactors)
	}
	if viewData.RecentCycleFactors[0].Key != models.CycleFactorStress || viewData.RecentCycleFactors[0].Count != 1 {
		t.Fatalf("expected stress to lead context, got %#v", viewData.RecentCycleFactors)
	}
}

func TestBuildStatsPageViewDataKeepsInsightsHiddenUntilSecondCompletedCycle(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := mustParseStatsServiceDay(t, "2026-02-10")

	viewData, err := service.BuildStatsPageViewData(&models.User{ID: 10, Role: models.RoleOwner, CycleLength: 28}, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.Flags.HasInsights {
		t.Fatalf("expected HasInsights=false with one completed cycle")
	}
	if viewData.ShowPredictionReliability {
		t.Fatalf("expected ShowPredictionReliability=false before base insights unlock")
	}
	if viewData.Flags.InsightProgress != 50 {
		t.Fatalf("expected InsightProgress=50, got %d", viewData.Flags.InsightProgress)
	}
}

func TestBuildStatsPageViewDataBuildsLastCycleSymptomsPatternsAndBBTChart(t *testing.T) {
	service, user, now := newStatsPatternAndBBTTestFixture(t)
	viewData, err := service.BuildStatsPageViewData(user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	assertStatsPatternAndBBTViewData(t, viewData)
}

func assertOwnerTrendViewData(t *testing.T, viewData StatsPageViewData) {
	t.Helper()

	assertOwnerTrendIdentity(t, viewData)
	assertOwnerTrendChartData(t, viewData)
	assertOwnerTrendReliability(t, viewData)
	assertOwnerTrendSymptomSummary(t, viewData)
}

func assertOwnerTrendIdentity(t *testing.T, viewData StatsPageViewData) {
	t.Helper()

	if !viewData.IsOwner {
		t.Fatalf("expected IsOwner=true")
	}
	if viewData.TrendPointCount != 2 {
		t.Fatalf("expected TrendPointCount=2, got %d", viewData.TrendPointCount)
	}
}

func assertOwnerTrendChartData(t *testing.T, viewData StatsPageViewData) {
	t.Helper()

	if viewData.ChartData.Kind != "bar" {
		t.Fatalf("expected chart kind=bar, got %q", viewData.ChartData.Kind)
	}
	if !viewData.ChartData.HasBaseline || viewData.ChartData.Baseline != 28 {
		t.Fatalf("expected chart baseline=28, got has=%v value=%d", viewData.ChartData.HasBaseline, viewData.ChartData.Baseline)
	}
	if viewData.ChartBaseline != 28 {
		t.Fatalf("expected ChartBaseline=28, got %d", viewData.ChartBaseline)
	}
	if len(viewData.ChartData.Labels) != 2 || viewData.ChartData.Labels[0] != "Cycle 1" || viewData.ChartData.Labels[1] != "Cycle 2" {
		t.Fatalf("unexpected chart labels: %#v", viewData.ChartData.Labels)
	}
	if len(viewData.ChartData.Values) != 2 || viewData.ChartData.Values[0] != 28 || viewData.ChartData.Values[1] != 28 {
		t.Fatalf("unexpected chart values: %#v", viewData.ChartData.Values)
	}
}

func assertOwnerTrendReliability(t *testing.T, viewData StatsPageViewData) {
	t.Helper()

	if !viewData.ShowPredictionReliability {
		t.Fatalf("expected prediction reliability block to be available")
	}
	if viewData.PredictionSampleCount != 3 {
		t.Fatalf("expected PredictionSampleCount=3, got %d", viewData.PredictionSampleCount)
	}
	if viewData.PredictionSampleUsesRecentWindow {
		t.Fatalf("expected uncapped sample count for three completed cycles")
	}
	if viewData.PredictionReliabilityLabelKey != "stats.reliability.building" {
		t.Fatalf("expected building reliability label, got %q", viewData.PredictionReliabilityLabelKey)
	}
}

func assertOwnerTrendSymptomSummary(t *testing.T, viewData StatsPageViewData) {
	t.Helper()

	if len(viewData.SymptomCounts) != 1 {
		t.Fatalf("expected one symptom count entry, got %d", len(viewData.SymptomCounts))
	}
	if viewData.SymptomCounts[0].FrequencySummary == "" {
		t.Fatalf("expected non-empty frequency summary")
	}
}

func newStatsPatternAndBBTTestFixture(t *testing.T) (*StatsService, *models.User, time.Time) {
	t.Helper()

	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-02"), SymptomIDs: []uint{1}},
		{Date: mustParseStatsServiceDay(t, "2026-01-05"), SymptomIDs: []uint{2}},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-30"), SymptomIDs: []uint{1}},
		{Date: mustParseStatsServiceDay(t, "2026-02-02"), SymptomIDs: []uint{2}},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-27"), SymptomIDs: []uint{1}},
		{Date: mustParseStatsServiceDay(t, "2026-02-28"), SymptomIDs: []uint{1}},
		{Date: mustParseStatsServiceDay(t, "2026-03-02"), SymptomIDs: []uint{2}},
		{Date: mustParseStatsServiceDay(t, "2026-03-04"), SymptomIDs: []uint{3}},
		{Date: mustParseStatsServiceDay(t, "2026-03-26"), IsPeriod: true, BBT: 36.40},
		{Date: mustParseStatsServiceDay(t, "2026-03-27"), BBT: 36.45},
		{Date: mustParseStatsServiceDay(t, "2026-03-28"), BBT: 36.50},
		{Date: mustParseStatsServiceDay(t, "2026-03-29"), BBT: 36.42},
		{Date: mustParseStatsServiceDay(t, "2026-03-30"), BBT: 36.43},
		{Date: mustParseStatsServiceDay(t, "2026-03-31"), BBT: 36.70},
		{Date: mustParseStatsServiceDay(t, "2026-04-01"), BBT: 36.72},
		{Date: mustParseStatsServiceDay(t, "2026-04-02"), BBT: 36.74},
	}

	service := NewStatsService(
		&stubStatsDayReader{logsForRange: logs, logsForAll: logs},
		&stubStatsSymptomReader{
			symptoms: []models.SymptomType{
				{ID: 1, Name: "Headache", Icon: "H"},
				{ID: 2, Name: "Cramps", Icon: "C"},
				{ID: 3, Name: "Acne", Icon: "A"},
			},
		},
	)

	currentCycleStart := mustParseStatsServiceDay(t, "2026-03-26")
	user := &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 28, TrackBBT: true, LastPeriodStart: &currentCycleStart}
	now := mustParseStatsServiceDay(t, "2026-04-02")
	return service, user, now
}

func assertStatsPatternAndBBTViewData(t *testing.T, viewData StatsPageViewData) {
	t.Helper()

	if !viewData.HasLastCycleSymptoms || len(viewData.LastCycleSymptoms) != 3 {
		t.Fatalf("expected last-cycle symptom summary, got %#v", viewData.LastCycleSymptoms)
	}
	if viewData.LastCycleSymptoms[0].Name != "Headache" {
		t.Fatalf("expected Headache to lead last-cycle symptoms, got %#v", viewData.LastCycleSymptoms)
	}
	if !viewData.HasSymptomPatterns || len(viewData.SymptomPatterns) != 2 {
		t.Fatalf("expected two symptom patterns, got %#v", viewData.SymptomPatterns)
	}
	if viewData.SymptomPatterns[0].Name != "Headache" || viewData.SymptomPatterns[0].DayStart != 2 || viewData.SymptomPatterns[0].DayEnd != 3 {
		t.Fatalf("expected Headache pattern on days 2-3, got %#v", viewData.SymptomPatterns[0])
	}
	if !viewData.HasCurrentCycleBBTChart {
		t.Fatalf("expected current-cycle BBT chart to be available")
	}
	if len(viewData.CurrentCycleBBTChart.Labels) != 8 {
		t.Fatalf("expected eight BBT chart labels, got %#v", viewData.CurrentCycleBBTChart.Labels)
	}
	if !viewData.CurrentCycleBBTChart.HasMarker || viewData.CurrentCycleBBTChart.MarkerIndex != 4 {
		t.Fatalf("expected probable ovulation marker on day 5, got %#v", viewData.CurrentCycleBBTChart)
	}
	if diff := viewData.CurrentCycleBBTChart.Baseline - 36.44; diff < -0.001 || diff > 0.001 {
		t.Fatalf("expected BBT baseline 36.44, got %.2f", viewData.CurrentCycleBBTChart.Baseline)
	}
}

func TestBuildStatsPageViewDataPartnerSkipsBaselineAndSymptomLoading(t *testing.T) {
	dayReader := &stubStatsDayReader{}
	service := NewStatsService(dayReader, &stubStatsSymptomReader{})

	partner := &models.User{ID: 9, Role: models.RolePartner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-02-21")

	viewData, err := service.BuildStatsPageViewData(partner, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}

	if viewData.IsOwner {
		t.Fatalf("expected IsOwner=false")
	}
	if viewData.ChartData.HasBaseline || viewData.ChartData.Baseline != 0 || viewData.ChartBaseline != 0 {
		t.Fatalf("expected no chart baseline for partner, got chart=%#v baseline=%d", viewData.ChartData, viewData.ChartBaseline)
	}
	if len(viewData.SymptomCounts) != 0 {
		t.Fatalf("expected no symptom counts for partner, got %#v", viewData.SymptomCounts)
	}
	if dayReader.fetchAllCalled {
		t.Fatalf("did not expect FetchAllLogsForUser for partner")
	}
}

func TestBuildStatsPageViewDataReturnsLoadStatsError(t *testing.T) {
	service := NewStatsService(&stubStatsDayReader{rangeErr: errors.New("range fail")}, &stubStatsSymptomReader{})
	user := &models.User{ID: 11, Role: models.RoleOwner, CycleLength: 28}

	_, err := service.BuildStatsPageViewData(user, "en", "Cycle %d", mustParseStatsServiceDay(t, "2026-02-21"), time.UTC, 12)
	if !errors.Is(err, ErrStatsPageViewLoadStats) {
		t.Fatalf("expected ErrStatsPageViewLoadStats, got %v", err)
	}
}

func TestBuildStatsPageViewDataReturnsLoadSymptomsError(t *testing.T) {
	dayReader := &stubStatsDayReader{logsForRange: []models.DailyLog{}}
	service := NewStatsService(dayReader, &stubStatsSymptomReader{err: errors.New("symptom fail")})
	user := &models.User{ID: 12, Role: models.RoleOwner, CycleLength: 28}

	_, err := service.BuildStatsPageViewData(user, "en", "Cycle %d", mustParseStatsServiceDay(t, "2026-02-21"), time.UTC, 12)
	if !errors.Is(err, ErrStatsPageViewLoadSymptoms) {
		t.Fatalf("expected ErrStatsPageViewLoadSymptoms, got %v", err)
	}
}
