package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// mutcalendarviewserviceCapturingStatsProvider records the `from` argument that
// BuildCalendarPageViewData passes into BuildCycleStatsForRange so the
// stats-range start (now-2y) can be asserted directly. Prefixed to avoid
// colliding with helpers in sibling mutation test files; the package-level
// stub/helper types (stubCalendarViewDayReader, mustParseCalendarViewDay) are
// reused from calendar_view_service_test.go and intentionally not redefined.
type mutcalendarviewserviceCapturingStatsProvider struct {
	stats        CycleStats
	capturedFrom time.Time
}

func (stub *mutcalendarviewserviceCapturingStatsProvider) BuildCycleStatsForRange(ctx context.Context, _ *models.User, from time.Time, _ time.Time, _ time.Time, _ *time.Location) (CycleStats, []models.DailyLog, error) {
	stub.capturedFrom = from
	return stub.stats, nil, nil
}

func TestBuildCalendarPageViewDataRequestsStatsRangeStartingTwoYearsBack(t *testing.T) {
	capturing := &mutcalendarviewserviceCapturingStatsProvider{}
	service := NewCalendarViewService(&stubCalendarViewDayReader{}, capturing)

	user := &models.User{ID: 1, Role: models.RoleOwner}
	now := mustParseCalendarViewDay(t, "2026-02-21")
	monthStart := mustParseCalendarViewDay(t, "2026-02-01")

	if _, err := service.BuildCalendarPageViewData(context.Background(), user, "en", now, monthStart, "", time.UTC); err != nil {
		t.Fatalf("BuildCalendarPageViewData() unexpected error: %v", err)
	}

	wantFrom := now.AddDate(-2, 0, 0)
	if !capturing.capturedFrom.Equal(wantFrom) {
		t.Fatalf("expected stats range start %s (two years back), got %s", wantFrom.Format("2006-01-02"), capturing.capturedFrom.Format("2006-01-02"))
	}
}

func TestBuildCalendarPageViewDataStatsRangeUsesNegativeTwoYearOffset(t *testing.T) {
	capturing := &mutcalendarviewserviceCapturingStatsProvider{}
	service := NewCalendarViewService(&stubCalendarViewDayReader{}, capturing)

	user := &models.User{ID: 7, Role: models.RoleOwner}
	now := mustParseCalendarViewDay(t, "2026-06-07")
	monthStart := mustParseCalendarViewDay(t, "2026-06-01")

	if _, err := service.BuildCalendarPageViewData(context.Background(), user, "en", now, monthStart, "", time.UTC); err != nil {
		t.Fatalf("BuildCalendarPageViewData() unexpected error: %v", err)
	}

	wantFrom := mustParseCalendarViewDay(t, "2024-06-07")
	if !capturing.capturedFrom.Equal(wantFrom) {
		t.Fatalf("expected stats range start 2024-06-07 (now minus two years), got %s", capturing.capturedFrom.Format("2006-01-02"))
	}
	if capturing.capturedFrom.After(now) {
		t.Fatalf("stats range start must precede now; got %s after now %s", capturing.capturedFrom.Format("2006-01-02"), now.Format("2006-01-02"))
	}
}

func TestBuildCalendarPageViewDataSecondaryExplanationAbsentWhenHintKeysEmpty(t *testing.T) {
	// Factor-bearing logs live in completed cycles (populating RecentCycles so the
	// explanation exists) but predate the recent 90-day window relative to now, so
	// HintFactorKeys is empty. The secondary explanation must therefore be absent.
	logs := []models.DailyLog{
		{Date: mustParseCalendarViewDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseCalendarViewDay(t, "2026-01-05"), CycleFactorKeys: []string{models.CycleFactorStress}},
		{Date: mustParseCalendarViewDay(t, "2026-01-25"), IsPeriod: true},
		{Date: mustParseCalendarViewDay(t, "2026-01-28"), CycleFactorKeys: []string{models.CycleFactorTravel}},
		{Date: mustParseCalendarViewDay(t, "2026-02-20"), IsPeriod: true},
	}
	service := NewCalendarViewService(
		&stubCalendarViewDayReader{},
		&stubCalendarViewStatsProvider{
			stats: CycleStats{
				CompletedCycleCount: 2,
				LastPeriodStart:     mustParseCalendarViewDay(t, "2026-02-20"),
				NextPeriodStart:     mustParseCalendarViewDay(t, "2026-03-24"),
				MedianCycleLength:   30,
				MinCycleLength:      20,
				MaxCycleLength:      44,
			},
			logs: logs,
		},
	)

	user := &models.User{ID: 3, Role: models.RoleOwner, IrregularCycle: true}
	now := mustParseCalendarViewDay(t, "2026-06-01")
	monthStart := mustParseCalendarViewDay(t, "2026-06-01")

	viewData, err := service.BuildCalendarPageViewData(context.Background(), user, "en", now, monthStart, "", time.UTC)
	if err != nil {
		t.Fatalf("BuildCalendarPageViewData() unexpected error: %v", err)
	}
	if viewData.HasPredictionExplanationSecondary {
		t.Fatalf("expected HasPredictionExplanationSecondary=false when HintFactorKeys is empty, got key=%q", viewData.PredictionExplanationSecondaryKey)
	}
	if viewData.PredictionExplanationSecondaryKey != "" {
		t.Fatalf("expected empty secondary key when HintFactorKeys is empty, got %q", viewData.PredictionExplanationSecondaryKey)
	}
}
