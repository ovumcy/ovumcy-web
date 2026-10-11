package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// On a rolled day the hero header follows MovedWindow.CurrentPhase, and the phase
// card marked current is the card that header names — here none, since the
// overview's "unknown" has no card in the running cycle's breakdown. The ribbon
// keeps the running cycle's geometry, so today's cell sits in the luteal card,
// which is not the highlighted one: the accepted split, rendered end to end by
// TestDashboardHeaderNamesTheOverviewPhaseWhileTheRibbonKeepsTheRunningCycle.
// Cycles of 24, 24, 24, 40 and 40 days, luteal phase 14, on cycle day 29.
func TestRolledWindowHeaderNamesTheOverviewPhaseAndMarksNoOtherCardCurrent(t *testing.T) {
	user := &models.User{ID: 53, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
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
	stats := BuildCycleStatsFromLogs(user, FilterLogsToStatsHistory(logs, today, time.UTC), now, time.UTC)
	published, _, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	hero := view.CycleHero
	if !hero.Visible || hero.CurrentDay != 29 || published.CurrentPhase != "unknown" {
		t.Fatalf("fixture: hero visible %t on day %d, overview phase %q; want a visible hero on day 29 and %q", hero.Visible, hero.CurrentDay, published.CurrentPhase, "unknown")
	}

	if hero.CurrentPhase != published.CurrentPhase {
		t.Errorf("hero header phase = %q, overview phase = %q, want one phase", hero.CurrentPhase, published.CurrentPhase)
	}
	for _, card := range hero.PhaseCards {
		if card.IsCurrent != (card.Phase == hero.CurrentPhase) {
			t.Errorf("card %q current = %t beside a header naming %q", card.Phase, card.IsCurrent, hero.CurrentPhase)
		}
	}
	var todayCell *DashboardCycleHeroDay
	for index := range hero.Days {
		if hero.Days[index].IsToday {
			todayCell = &hero.Days[index]
		}
	}
	if todayCell == nil || todayCell.Phase != "luteal" {
		t.Errorf("today's ribbon cell = %+v, want the running cycle's luteal span", todayCell)
	}
}
