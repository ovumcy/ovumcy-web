package api

import (
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestARecordedPeriodDayIsNeverMenstrualAndFertileOnTheWire pins WEB-434 on
// the HTTP surfaces: the running cycle started ten days ago (bleeding through
// day 5) and today is a period day. The API overview, the /stats card and the
// dashboard status line all said "menstrual" and "fertile" at once.
//   - "Mark anyway" (an uncertain start) counts as no mark, so the lone day
//     opens the cycle: day 1, no fertility claim.
//   - Spotting opens no cycle; the fertility half is withheld under
//     bleeding_outside_cycle_start and the phase stays menstrual.
func TestARecordedPeriodDayIsNeverMenstrualAndFertileOnTheWire(t *testing.T) {
	for _, tc := range []struct {
		name         string
		flow         string
		wantCycleDay int
		wantReason   bool
	}{
		{name: "forced-start", flow: models.FlowMedium, wantCycleDay: 1},
		{name: "spotting", flow: models.FlowSpotting, wantCycleDay: 11, wantReason: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user, authCookie, today := newStatsOverviewOwner(t, app, database, "web434-"+tc.name+"@example.com")
			updateStatsOverviewUser(t, database, user, map[string]any{
				"last_period_start": services.AddCalendarDays(today, -10, time.UTC),
			})
			seedStatsOverviewCycleHistory(t, database, user, today, 94, 66, 38, 10)
			for _, daysBack := range []int{9, 8, 7, 6} {
				seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: services.AddCalendarDays(today, -daysBack, time.UTC), IsPeriod: true, Flow: models.FlowMedium})
			}
			seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: today, IsPeriod: true, CycleStart: true, IsUncertain: true, Flow: tc.flow})

			_, payload := fetchStatsOverview(t, app, authCookie)
			if payload.CurrentCycleDay != tc.wantCycleDay {
				t.Errorf("current_cycle_day = %d, want %d", payload.CurrentCycleDay, tc.wantCycleDay)
			}
			if payload.CurrentPhase != "menstrual" {
				t.Errorf("current_phase = %q, want menstrual on a recorded period day", payload.CurrentPhase)
			}
			if payload.CurrentFertility == services.FertilityStatusFertile {
				t.Error("current_fertility = fertile beside current_phase menstrual")
			}
			hasReason := slices.Contains(payload.Suppression.Reasons, string(services.SuppressionReasonBleedingOutsideCycleStart))
			if hasReason != tc.wantReason {
				t.Errorf("suppression = %+v, want %s named: %v", payload.Suppression, services.SuppressionReasonBleedingOutsideCycleStart, tc.wantReason)
			}
			if tc.wantReason && (!payload.Suppression.Fertility || payload.OvulationDate != nil) {
				t.Errorf("suppression.fertility = %v, ovulation_date = %v; want withheld", payload.Suppression.Fertility, payload.OvulationDate)
			}

			statsPage := fetchStatsPageDocument(t, app, authCookie)
			if node := findHTMLNodeWithAttr(statsPage, "data-fertility-status"); node != nil && htmlAttr(node, "data-fertility-status") == services.FertilityStatusFertile {
				t.Error("/stats renders fertility fertile")
			}
			if findHTMLNodeWithAttr(statsPage, "data-fertile-window") != nil {
				t.Error("/stats renders the fertile-window line on a recorded period day")
			}

			dashboard := fetchStatsOverviewDashboardDocument(t, app, authCookie)
			if node := findHTMLNodeWithAttr(dashboard, "data-fertility-status"); node != nil && htmlAttr(node, "data-fertility-status") == services.FertilityStatusFertile {
				t.Error("the dashboard hero renders fertility fertile")
			}
			for _, attr := range []string{"data-fertile-window", "data-fertility-badge"} {
				if findHTMLNodeWithAttr(dashboard, attr) != nil {
					t.Errorf("the dashboard renders %s on a recorded period day", attr)
				}
			}
		})
	}
}
