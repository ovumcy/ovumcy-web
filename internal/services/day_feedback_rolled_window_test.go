package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Cycles of 24, 24, 24, 40 and 40 days, luteal phase 14, saved on cycle day 29:
// the running cycle's window (days 6-11) is long behind, and the window the
// overview, the calendar and the dashboard roll to (days 29-34) covers today. The
// save message read the running window and answered neutral beside a dashboard
// header calling today fertile.
func TestDayFeedbackCallsTodayFertileInsideTheRolledWindowLikeTheDashboard(t *testing.T) {
	user := &models.User{ID: 51, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
	today := time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC)
	logs := make([]models.DailyLog, 0, 6)
	for _, back := range []int{180, 156, 132, 108, 68, 28} {
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, -back), IsPeriod: true, Flow: models.FlowMedium, CycleStart: true})
	}

	now := localNoon(today, time.UTC)
	dashboard := NewDashboardViewService(
		NewStatsService(nil, nil),
		&stubDashboardViewerProvider{logEntry: models.DailyLog{Date: now}},
		&stubDashboardDayStateProvider{logs: logs},
	)
	view, err := dashboard.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if view.Stats.CurrentCycleDay != 29 || !view.ShowFertilityStatus {
		t.Fatalf("fixture: cycle day %d, fertility shown %t; want day 29 with the status shown", view.Stats.CurrentCycleDay, view.ShowFertilityStatus)
	}
	if view.Stats.CurrentFertility != FertilityStatusFertile {
		t.Fatalf("dashboard fertility = %q, want %q off the rolled window", view.Stats.CurrentFertility, FertilityStatusFertile)
	}

	if got := dayFeedbackKeyOn(t, user, logs, time.UTC, today, today); got != daySaveMessageFertile {
		t.Fatalf("save message on cycle day 29 = %q, want %q, as the dashboard header says", got, daySaveMessageFertile)
	}
}

// The same history on cycle day 25: the ovulation has rolled to cycle day 34,
// so the window the overview publishes (days 29-34) is still AHEAD of today.
// The dashboard publishes that window with the status read against it, never
// the running cycle's window (days 6-11) beside a status that is not about it,
// and the save message agrees.
func TestRolledWindowAheadOfTodayKeepsTheDashboardAndTheSaveMessageOnTheOverview(t *testing.T) {
	user := &models.User{ID: 52, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
	today := time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC)
	logs := make([]models.DailyLog, 0, 6)
	for _, back := range []int{176, 152, 128, 104, 64, 24} {
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, -back), IsPeriod: true, Flow: models.FlowMedium, CycleStart: true})
	}
	now := localNoon(today, time.UTC)
	stats := BuildCycleStatsFromLogs(user, FilterLogsToStatsHistory(logs, today, time.UTC), now, time.UTC)

	published, suppression, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if _, _, moved := ConfirmedAndOverviewStats(user, logs, stats, today, time.UTC); suppression.FertilitySuppressed || !moved.Reconciled {
		t.Fatalf("fixture: suppression %+v; want the window rolled and published", suppression)
	}
	if published.FertilityWindowStart.IsZero() || CalendarDaysBetween(today, published.FertilityWindowStart) <= 0 {
		t.Fatalf("fixture: overview window starts %s, want a day after %s", CalendarDayKey(published.FertilityWindowStart), CalendarDayKey(today))
	}
	if published.CurrentFertility == FertilityStatusFertile {
		t.Fatalf("overview calls today fertile ahead of its own window %s..%s", CalendarDayKey(published.FertilityWindowStart), CalendarDayKey(published.FertilityWindowEnd))
	}

	view := rolledWindowDashboardView(t, user, logs, now)
	if view.Stats.CurrentCycleDay != 25 {
		t.Fatalf("fixture: cycle day %d, want 25", view.Stats.CurrentCycleDay)
	}
	assertDashboardStatsOnTheOverview(t, view.Stats, published)

	if got := dayFeedbackKeyOn(t, user, logs, time.UTC, today, today); got != daySaveMessageNeutral {
		t.Fatalf("save message on cycle day 25 = %q, want %q ahead of the rolled window", got, daySaveMessageNeutral)
	}
}

// Three 28-day cycles and a running one from 9999-12-10, saved on 9999-12-30:
// the running ovulation (9999-12-23) is behind today and the day it rolls to
// falls past 9999-12-31, so the overview publishes no window and an unknown
// status. The dashboard drops the running window with it, and the save message
// names no fertile day off a window no surface publishes.
func TestRolledWindowPastYear9999LeavesTheDashboardAndTheSaveMessageWithoutAWindow(t *testing.T) {
	user := &models.User{ID: 53, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
	lastStart := time.Date(9999, time.December, 10, 0, 0, 0, 0, time.UTC)
	today := time.Date(9999, time.December, 30, 0, 0, 0, 0, time.UTC)
	logs := make([]models.DailyLog, 0, 20)
	for _, back := range []int{84, 56, 28, 0} {
		start := lastStart.AddDate(0, 0, -back)
		for offset := range 5 {
			logs = append(logs, models.DailyLog{UserID: user.ID, Date: start.AddDate(0, 0, offset), IsPeriod: true, Flow: models.FlowMedium, CycleStart: offset == 0})
		}
	}
	now := localNoon(today, time.UTC)
	stats := BuildCycleStatsFromLogs(user, FilterLogsToStatsHistory(logs, today, time.UTC), now, time.UTC)

	published, _, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if _, _, moved := ConfirmedAndOverviewStats(user, logs, stats, today, time.UTC); !moved.Reconciled {
		t.Fatal("fixture: want the ovulation rolled past the year")
	}
	if !published.FertilityWindowStart.IsZero() || published.CurrentFertility != FertilityStatusUnknown {
		t.Fatalf("fixture: overview window %s, fertility %q; want none and %q", CalendarDayKey(published.FertilityWindowStart), published.CurrentFertility, FertilityStatusUnknown)
	}

	assertDashboardStatsOnTheOverview(t, rolledWindowDashboardView(t, user, logs, now).Stats, published)

	if got := dayFeedbackKeyOn(t, user, logs, time.UTC, today, today); got != daySaveMessageNeutral {
		t.Fatalf("save message on 9999-12-30 = %q, want %q with no window published", got, daySaveMessageNeutral)
	}
}

func rolledWindowDashboardView(t *testing.T, user *models.User, logs []models.DailyLog, now time.Time) DashboardViewData {
	t.Helper()
	dashboard := NewDashboardViewService(
		NewStatsService(nil, nil),
		&stubDashboardViewerProvider{logEntry: models.DailyLog{Date: now}},
		&stubDashboardDayStateProvider{logs: logs},
	)
	view, err := dashboard.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	return view
}

func assertDashboardStatsOnTheOverview(t *testing.T, got CycleStats, overview CycleStats) {
	t.Helper()
	if got.CurrentFertility != overview.CurrentFertility || got.CurrentPhase != overview.CurrentPhase {
		t.Errorf("dashboard fertility/phase = %q/%q, overview %q/%q", got.CurrentFertility, got.CurrentPhase, overview.CurrentFertility, overview.CurrentPhase)
	}
	if !sameDay(got.NextPeriodStart, overview.NextPeriodStart) {
		t.Errorf("dashboard next period %s, overview %s", CalendarDayKey(got.NextPeriodStart), CalendarDayKey(overview.NextPeriodStart))
	}
	if !sameDay(got.FertilityWindowStart, overview.FertilityWindowStart) || !sameDay(got.FertilityWindowEnd, overview.FertilityWindowEnd) || !sameDay(got.OvulationDate, overview.OvulationDate) {
		t.Errorf("dashboard window %s..%s ovulation %s, overview %s..%s ovulation %s",
			CalendarDayKey(got.FertilityWindowStart), CalendarDayKey(got.FertilityWindowEnd), CalendarDayKey(got.OvulationDate),
			CalendarDayKey(overview.FertilityWindowStart), CalendarDayKey(overview.FertilityWindowEnd), CalendarDayKey(overview.OvulationDate))
	}
}
