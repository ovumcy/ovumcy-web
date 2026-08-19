package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubCalendarViewDayReader struct {
	logs []models.DailyLog
	err  error

	// Captured userID argument — used to prove the day read is owner-scoped.
	logsUserID uint
}

func (stub *stubCalendarViewDayReader) FetchLogsForUser(ctx context.Context, userID uint, _ time.Time, _ time.Time, _ *time.Location) ([]models.DailyLog, error) {
	stub.logsUserID = userID
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]models.DailyLog, len(stub.logs))
	copy(result, stub.logs)
	return result, nil
}

type stubCalendarViewStatsProvider struct {
	stats CycleStats
	logs  []models.DailyLog
	err   error

	// Captured owner argument — used to prove the stats build is owner-scoped.
	statsUserID uint
}

func (stub *stubCalendarViewStatsProvider) BuildCycleStatsForRange(ctx context.Context, user *models.User, _ time.Time, _ time.Time, _ time.Time, _ *time.Location) (CycleStats, []models.DailyLog, error) {
	if user != nil {
		stub.statsUserID = user.ID
	}
	if stub.err != nil {
		return CycleStats{}, nil, stub.err
	}
	logs := make([]models.DailyLog, len(stub.logs))
	copy(logs, stub.logs)
	return stub.stats, logs, nil
}

func TestBuildCalendarPageViewData(t *testing.T) {
	dayReader := &stubCalendarViewDayReader{}
	statsProvider := &stubCalendarViewStatsProvider{stats: CycleStats{MedianCycleLength: 28}}
	service := NewCalendarViewService(dayReader, statsProvider)

	// The id is deliberately not 1: an owner argument replaced by a constant must not
	// coincide with the authenticated owner's id, or the guard below cannot go red.
	user := &models.User{ID: 4242, Role: models.RoleOwner}
	now := mustParseCalendarViewDay(t, "2026-02-21")
	monthStart := mustParseCalendarViewDay(t, "2026-02-01")

	viewData, err := service.BuildCalendarPageViewData(context.Background(), user, "en", now, monthStart, "2026-02-17", time.UTC)
	if err != nil {
		t.Fatalf("BuildCalendarPageViewData() unexpected error: %v", err)
	}

	if viewData.MonthValue != "2026-02" {
		t.Fatalf("expected MonthValue=2026-02, got %q", viewData.MonthValue)
	}
	if viewData.PrevMonth != "2026-01" || viewData.NextMonth != "2026-03" {
		t.Fatalf("unexpected adjacent months prev=%q next=%q", viewData.PrevMonth, viewData.NextMonth)
	}
	if viewData.SelectedDate != "2026-02-17" {
		t.Fatalf("expected selected date passthrough, got %q", viewData.SelectedDate)
	}
	if viewData.TodayISO != "2026-02-21" {
		t.Fatalf("expected TodayISO=2026-02-21, got %q", viewData.TodayISO)
	}
	if !viewData.IsOwner {
		t.Fatalf("expected IsOwner=true")
	}
	if len(viewData.DayStates) == 0 {
		t.Fatalf("expected non-empty day states")
	}

	// Privacy boundary: both collaborators must be handed the authenticated owner, and the
	// same one. A zero id is invalid input, never a reason to treat the read as unscoped.
	if dayReader.logsUserID == 0 {
		t.Fatalf("day read received no owner id")
	}
	if dayReader.logsUserID != user.ID {
		t.Fatalf("expected day read scoped to owner id %d, got %d", user.ID, dayReader.logsUserID)
	}
	if statsProvider.statsUserID != user.ID {
		t.Fatalf("expected stats build scoped to owner id %d, got %d", user.ID, statsProvider.statsUserID)
	}
	if statsProvider.statsUserID != dayReader.logsUserID {
		t.Fatalf(
			"day read and stats build disagree on the owner: day=%d stats=%d",
			dayReader.logsUserID,
			statsProvider.statsUserID,
		)
	}
}

func TestBuildCalendarPageViewDataReturnsTypedErrors(t *testing.T) {
	user := &models.User{ID: 2, Role: models.RoleOwner}
	now := mustParseCalendarViewDay(t, "2026-02-21")
	monthStart := mustParseCalendarViewDay(t, "2026-02-01")

	logErrService := NewCalendarViewService(
		&stubCalendarViewDayReader{err: errors.New("logs fail")},
		&stubCalendarViewStatsProvider{},
	)
	if _, err := logErrService.BuildCalendarPageViewData(context.Background(), user, "en", now, monthStart, "", time.UTC); !errors.Is(err, ErrCalendarViewLoadLogs) {
		t.Fatalf("expected ErrCalendarViewLoadLogs, got %v", err)
	}

	statsErrService := NewCalendarViewService(
		&stubCalendarViewDayReader{},
		&stubCalendarViewStatsProvider{err: errors.New("stats fail")},
	)
	if _, err := statsErrService.BuildCalendarPageViewData(context.Background(), user, "en", now, monthStart, "", time.UTC); !errors.Is(err, ErrCalendarViewLoadStats) {
		t.Fatalf("expected ErrCalendarViewLoadStats, got %v", err)
	}
}

func TestBuildCalendarPageViewDataHidesPreviousMonthAtLowerBound(t *testing.T) {
	service := NewCalendarViewService(
		&stubCalendarViewDayReader{},
		&stubCalendarViewStatsProvider{},
	)

	user := &models.User{
		ID:        9,
		Role:      models.RoleOwner,
		CreatedAt: time.Date(2026, time.March, 13, 12, 0, 0, 0, time.UTC),
	}
	now := mustParseCalendarViewDay(t, "2026-03-13")
	monthStart := time.Date(2023, time.March, 1, 0, 0, 0, 0, time.UTC)

	viewData, err := service.BuildCalendarPageViewData(context.Background(), user, "en", now, monthStart, "", time.UTC)
	if err != nil {
		t.Fatalf("BuildCalendarPageViewData() unexpected error: %v", err)
	}
	if viewData.PrevMonth != "" {
		t.Fatalf("expected empty prev month at lower bound, got %q", viewData.PrevMonth)
	}
	if viewData.NextMonth != "2023-04" {
		t.Fatalf("expected next month 2023-04, got %q", viewData.NextMonth)
	}
}

func TestBuildCalendarPageViewDataBuildsSharedPredictionExplanation(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseCalendarViewDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseCalendarViewDay(t, "2026-01-03"), CycleFactorKeys: []string{models.CycleFactorStress}},
		{Date: mustParseCalendarViewDay(t, "2026-01-25"), IsPeriod: true},
		{Date: mustParseCalendarViewDay(t, "2026-01-28"), CycleFactorKeys: []string{models.CycleFactorTravel}},
		{Date: mustParseCalendarViewDay(t, "2026-03-10"), IsPeriod: true},
		{Date: mustParseCalendarViewDay(t, "2026-03-12"), CycleFactorKeys: []string{models.CycleFactorStress}},
		{Date: mustParseCalendarViewDay(t, "2026-04-20"), IsPeriod: true},
	}
	service := NewCalendarViewService(
		&stubCalendarViewDayReader{},
		&stubCalendarViewStatsProvider{
			stats: CycleStats{
				CompletedCycleCount: 3,
				LastPeriodStart:     mustParseCalendarViewDay(t, "2026-04-20"),
				NextPeriodStart:     mustParseCalendarViewDay(t, "2026-05-22"),
				MedianCycleLength:   32,
				MinCycleLength:      24,
				MaxCycleLength:      44,
			},
			logs: logs,
		},
	)

	user := &models.User{ID: 3, Role: models.RoleOwner, IrregularCycle: true}
	now := mustParseCalendarViewDay(t, "2026-04-25")
	monthStart := mustParseCalendarViewDay(t, "2026-04-01")

	viewData, err := service.BuildCalendarPageViewData(context.Background(), user, "en", now, monthStart, "2026-04-25", time.UTC)
	if err != nil {
		t.Fatalf("BuildCalendarPageViewData() unexpected error: %v", err)
	}
	if !viewData.HasPredictionExplanationPrimary || viewData.PredictionExplanationPrimaryKey != "prediction.explainer.irregular_ranges" {
		t.Fatalf("expected shared irregular range explanation, got %#v", viewData)
	}
	if !viewData.HasPredictionExplanationSecondary || viewData.PredictionExplanationSecondaryKey != "prediction.explainer.factor_context" {
		t.Fatalf("expected shared factor explanation, got %#v", viewData)
	}
}

func mustParseCalendarViewDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}
