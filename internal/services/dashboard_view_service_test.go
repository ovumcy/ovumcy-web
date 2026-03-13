package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type stubDashboardStatsProvider struct {
	stats CycleStats
	err   error
}

func (stub *stubDashboardStatsProvider) BuildCycleStatsForRange(_ *models.User, _ time.Time, _ time.Time, _ time.Time, _ *time.Location) (CycleStats, []models.DailyLog, error) {
	if stub.err != nil {
		return CycleStats{}, nil, stub.err
	}
	return stub.stats, nil, nil
}

type stubDashboardViewerProvider struct {
	logEntry models.DailyLog
	symptoms []models.SymptomType
	err      error
}

func (stub *stubDashboardViewerProvider) FetchDayLogForViewer(_ *models.User, _ time.Time, _ *time.Location) (models.DailyLog, []models.SymptomType, error) {
	if stub.err != nil {
		return models.DailyLog{}, nil, stub.err
	}
	symptoms := make([]models.SymptomType, len(stub.symptoms))
	copy(symptoms, stub.symptoms)
	return stub.logEntry, symptoms, nil
}

type stubDashboardDayStateProvider struct {
	hasData bool
	err     error
	logs    []models.DailyLog
}

func (stub *stubDashboardDayStateProvider) DayHasDataForDate(_ uint, _ time.Time, _ *time.Location) (bool, error) {
	if stub.err != nil {
		return false, stub.err
	}
	return stub.hasData, nil
}

func (stub *stubDashboardDayStateProvider) FetchAllLogsForUser(_ uint) ([]models.DailyLog, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	logs := make([]models.DailyLog, len(stub.logs))
	copy(logs, stub.logs)
	return logs, nil
}

func TestBuildDashboardViewData(t *testing.T) {
	user := &models.User{ID: 1, Role: models.RoleOwner, CycleLength: 28}
	today := mustParseDashboardServiceDay(t, "2026-02-21")
	stats := CycleStats{
		CurrentCycleDay:   5,
		MedianCycleLength: 28,
	}

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{stats: stats},
		&stubDashboardViewerProvider{
			logEntry: models.DailyLog{
				Date:       today,
				IsPeriod:   false,
				Flow:       models.FlowNone,
				Notes:      "note",
				SymptomIDs: []uint{3},
			},
			symptoms: []models.SymptomType{{ID: 3, Name: "Headache"}},
		},
		&stubDashboardDayStateProvider{},
	)

	viewData, err := service.BuildDashboardViewData(user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if viewData.Today.Format("2006-01-02") != "2026-02-21" {
		t.Fatalf("expected Today=2026-02-21, got %s", viewData.Today.Format("2006-01-02"))
	}
	if !viewData.IsOwner {
		t.Fatalf("expected IsOwner=true")
	}
	if !viewData.TodayHasData {
		t.Fatalf("expected TodayHasData=true")
	}
	if len(viewData.Symptoms) != 1 {
		t.Fatalf("expected one symptom in view data, got %d", len(viewData.Symptoms))
	}
	if !viewData.SelectedSymptomID[3] {
		t.Fatalf("expected selected symptom id=3")
	}
	if !viewData.AllowManualCycleStart {
		t.Fatalf("expected AllowManualCycleStart=true for today")
	}
}

func TestBuildDashboardViewDataReturnsTypedErrors(t *testing.T) {
	user := &models.User{ID: 2, Role: models.RoleOwner}
	now := mustParseDashboardServiceDay(t, "2026-02-21")

	statsErrService := NewDashboardViewService(
		&stubDashboardStatsProvider{err: errors.New("stats fail")},
		&stubDashboardViewerProvider{},
		&stubDashboardDayStateProvider{},
	)
	if _, err := statsErrService.BuildDashboardViewData(user, "en", now, time.UTC); !errors.Is(err, ErrDashboardViewLoadStats) {
		t.Fatalf("expected ErrDashboardViewLoadStats, got %v", err)
	}

	dayErrService := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{err: errors.New("day fail")},
		&stubDashboardDayStateProvider{},
	)
	if _, err := dayErrService.BuildDashboardViewData(user, "en", now, time.UTC); !errors.Is(err, ErrDashboardViewLoadTodayLog) {
		t.Fatalf("expected ErrDashboardViewLoadTodayLog, got %v", err)
	}
}

func TestBuildDayEditorViewData(t *testing.T) {
	user := &models.User{ID: 3, Role: models.RoleOwner}
	now := mustParseDashboardServiceDay(t, "2026-02-21")
	day := mustParseDashboardServiceDay(t, "2026-02-22")

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{
			logEntry: models.DailyLog{
				Date:       day,
				IsPeriod:   true,
				Flow:       models.FlowLight,
				SymptomIDs: []uint{7},
			},
			symptoms: []models.SymptomType{{ID: 7, Name: "Bloating"}},
		},
		&stubDashboardDayStateProvider{hasData: true},
	)

	viewData, err := service.BuildDayEditorViewData(user, "en", day, now, time.UTC)
	if err != nil {
		t.Fatalf("BuildDayEditorViewData() unexpected error: %v", err)
	}
	if !viewData.IsFutureDate {
		t.Fatalf("expected IsFutureDate=true")
	}
	if !viewData.HasDayData {
		t.Fatalf("expected HasDayData=true")
	}
	if viewData.DateString != "2026-02-22" {
		t.Fatalf("expected DateString=2026-02-22, got %q", viewData.DateString)
	}
	if !viewData.SelectedSymptomID[7] {
		t.Fatalf("expected selected symptom id=7")
	}
	if !viewData.AllowManualCycleStart {
		t.Fatalf("expected AllowManualCycleStart=true for tomorrow")
	}
	if !viewData.ShowFutureCycleStartNotice {
		t.Fatalf("expected ShowFutureCycleStartNotice=true for tomorrow")
	}
}

func TestBuildDashboardViewDataSuggestsManualCycleStartAfterLongGap(t *testing.T) {
	user := &models.User{ID: 5, Role: models.RoleOwner, CycleLength: 28}
	today := mustParseDashboardServiceDay(t, "2026-02-21")

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{
			logEntry: models.DailyLog{
				Date:     today,
				IsPeriod: true,
				Flow:     models.FlowLight,
			},
			symptoms: []models.SymptomType{{ID: 3, Name: "Headache"}},
		},
		&stubDashboardDayStateProvider{
			logs: []models.DailyLog{
				{Date: mustParseDashboardServiceDay(t, "2026-02-01"), IsPeriod: true, CycleStart: true},
				{Date: today, IsPeriod: true},
			},
		},
	)

	viewData, err := service.BuildDashboardViewData(user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if !viewData.ShowCycleStartSuggestion {
		t.Fatalf("expected ShowCycleStartSuggestion=true after a long gap")
	}
}

func TestBuildDashboardViewDataShowsHighFertilityBadgeForEggWhiteMucus(t *testing.T) {
	user := &models.User{ID: 6, Role: models.RoleOwner, CycleLength: 28, TrackCervicalMucus: true}
	today := mustParseDashboardServiceDay(t, "2026-02-21")

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{
			logEntry: models.DailyLog{
				Date:          today,
				CervicalMucus: models.CervicalMucusEggWhite,
			},
		},
		&stubDashboardDayStateProvider{},
	)

	viewData, err := service.BuildDashboardViewData(user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if !viewData.ShowHighFertilityBadge {
		t.Fatalf("expected high-fertility badge for egg-white mucus")
	}
}

func TestBuildDayEditorViewDataReturnsTypedErrors(t *testing.T) {
	user := &models.User{ID: 4, Role: models.RoleOwner}
	now := mustParseDashboardServiceDay(t, "2026-02-21")
	day := mustParseDashboardServiceDay(t, "2026-02-22")

	dayStateErrService := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{},
		&stubDashboardDayStateProvider{err: errors.New("state fail")},
	)
	if _, err := dayStateErrService.BuildDayEditorViewData(user, "en", day, now, time.UTC); !errors.Is(err, ErrDashboardViewLoadDayState) {
		t.Fatalf("expected ErrDashboardViewLoadDayState, got %v", err)
	}

	dayLogErrService := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{err: errors.New("day log fail")},
		&stubDashboardDayStateProvider{},
	)
	if _, err := dayLogErrService.BuildDayEditorViewData(user, "en", day, now, time.UTC); !errors.Is(err, ErrDashboardViewLoadDayLog) {
		t.Fatalf("expected ErrDashboardViewLoadDayLog, got %v", err)
	}
}

func mustParseDashboardServiceDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}
