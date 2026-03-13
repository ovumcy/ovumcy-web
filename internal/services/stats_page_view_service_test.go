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
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := mustParseStatsServiceDay(t, "2026-03-20")

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
	if regularView.ChartBaseline != 34 {
		t.Fatalf("expected averaged chart baseline 34, got %d", regularView.ChartBaseline)
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

func assertOwnerTrendViewData(t *testing.T, viewData StatsPageViewData) {
	t.Helper()

	if !viewData.IsOwner {
		t.Fatalf("expected IsOwner=true")
	}
	if viewData.ChartData.Kind != "bar" {
		t.Fatalf("expected chart kind=bar, got %q", viewData.ChartData.Kind)
	}
	if !viewData.ChartData.HasBaseline || viewData.ChartData.Baseline != 28 {
		t.Fatalf("expected chart baseline=28, got has=%v value=%d", viewData.ChartData.HasBaseline, viewData.ChartData.Baseline)
	}
	if viewData.ChartBaseline != 28 {
		t.Fatalf("expected ChartBaseline=28, got %d", viewData.ChartBaseline)
	}
	if viewData.TrendPointCount != 2 {
		t.Fatalf("expected TrendPointCount=2, got %d", viewData.TrendPointCount)
	}
	if len(viewData.ChartData.Labels) != 2 || viewData.ChartData.Labels[0] != "Cycle 1" || viewData.ChartData.Labels[1] != "Cycle 2" {
		t.Fatalf("unexpected chart labels: %#v", viewData.ChartData.Labels)
	}
	if len(viewData.ChartData.Values) != 2 || viewData.ChartData.Values[0] != 28 || viewData.ChartData.Values[1] != 28 {
		t.Fatalf("unexpected chart values: %#v", viewData.ChartData.Values)
	}
	if len(viewData.SymptomCounts) != 1 {
		t.Fatalf("expected one symptom count entry, got %d", len(viewData.SymptomCounts))
	}
	if viewData.SymptomCounts[0].FrequencySummary == "" {
		t.Fatalf("expected non-empty frequency summary")
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
