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
