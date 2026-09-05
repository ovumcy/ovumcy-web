package api

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestStatsOverviewConfirmsALateShiftAfterTheProjectedNextPeriodStart is the
// JSON-API half of the current-cycle detection window fix
// (services.ConfirmedCurrentCycleOvulation, cycle_signals.go): a thermal shift
// whose coverline window straddles the model's own projected next period start
// is still an event of the CURRENT cycle, and GET /api/v1/stats/overview must
// confirm it like the calendar and the dashboard already do.
//
// Before the fix, the detector's series was bounded at stats.NextPeriodStart
// itself, which cut the 6-day coverline window one day short of full and the
// shift was never seen at all: the endpoint answered the model's stale
// projection instead of the measured day.
func TestStatsOverviewConfirmsALateShiftAfterTheProjectedNextPeriodStart(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "overview-late-shift@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	updateStatsOverviewUser(t, database, user, map[string]any{"track_bbt": true})

	// Three prior 28-day cycles fix the median at 28; the fourth start opens the
	// CURRENT cycle at today-31 (cycle day 32 today, not overdue: 32 <= 28+7).
	// The median projects the next period on today-3 — the exact day the
	// coverline window's 6th (and last) undisturbed reading falls on.
	seedStatsOverviewCycleHistory(t, database, user, today, 115, 87, 59, 31)

	// Undisturbed temperatures fill the coverline window on cycle days 24-29
	// (today-8..today-3, the projected next period start itself); the elevated
	// streak follows on cycle days 30-32 (today-2..today). Bounding the
	// detector's window at the projection excludes today-3 — the 6th coverline
	// reading — so the window never fills and the shift goes unseen; bounding
	// it at today+1 (this fix) includes it.
	for _, offset := range []int{8, 7, 6, 5, 4, 3} {
		seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, -offset), BBT: new(36.20)})
	}
	for _, offset := range []int{2, 1, 0} {
		seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, -offset), BBT: new(36.50)})
	}

	_, payload := fetchStatsOverview(t, app, authCookie)

	wantConfirmed := today.AddDate(0, 0, -3).Format(statsOverviewDateLayout)
	if payload.OvulationDate == nil || *payload.OvulationDate != wantConfirmed {
		t.Fatalf("ovulation_date = %v, want the BBT-confirmed %s (a shift recorded after the projected next period start is still this cycle's)", payload.OvulationDate, wantConfirmed)
	}
	if !payload.OvulationConfirmed {
		t.Fatal("ovulation_confirmed = false beside a BBT-confirmed ovulation_date recorded after the projected next period start")
	}
	if payload.Suppression.Fertility {
		t.Fatalf("suppression.fertility = true for a not-yet-overdue cycle with a confirmed shift: %v", payload.Suppression.Reasons)
	}
}
