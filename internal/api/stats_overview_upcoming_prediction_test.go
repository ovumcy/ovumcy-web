package api

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestStatsOverviewNamesTheOvulationTheDashboardRendersOnceItHasPassed reads
// GET /api/v1/stats/overview and the rendered dashboard for one
// trying-to-conceive owner with three completed 28-day cycles, on a day the
// running cycle's ovulation is already behind. The dashboard rolls that
// ovulation a cycle forward; the overview used to publish the passed day,
// flagged exact. Cycle day 29 is the reported case — the period that closes the
// running cycle is due today — and cycle day 22 the same divergence before the
// cycle has reached its own length.
func TestStatsOverviewNamesTheOvulationTheDashboardRendersOnceItHasPassed(t *testing.T) {
	for _, testCase := range []struct {
		cycleDay int
		// Days from today, by the 28-day arithmetic: the next period closes the
		// running cycle, the ovulation is day 14 of the cycle after it.
		nextPeriodIn int
		ovulationIn  int
	}{
		{cycleDay: 22, nextPeriodIn: 7, ovulationIn: 20},
		{cycleDay: 29, nextPeriodIn: 0, ovulationIn: 13},
	} {
		t.Run(fmt.Sprintf("cycle day %d", testCase.cycleDay), func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user := createOnboardingTestUser(t, database, fmt.Sprintf("overview-upcoming-%d@example.com", testCase.cycleDay), "StrongPass1", true)
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
			// The rendered ovulation slot exists only for an account tracking to
			// conceive.
			updateStatsOverviewUser(t, database, user, map[string]any{"usage_goal": models.UsageGoalTrying})
			running := testCase.cycleDay - 1
			seedStatsOverviewCycleHistory(t, database, user, today, running+84, running+56, running+28, running)

			_, payload := fetchStatsOverview(t, app, authCookie)
			if payload.CurrentCycleDay != testCase.cycleDay {
				t.Fatalf("fixture: current_cycle_day = %d, want %d", payload.CurrentCycleDay, testCase.cycleDay)
			}
			if payload.Suppression.Predictions || payload.Suppression.Fertility {
				t.Fatalf("fixture: suppression = %+v, want the projection published", payload.Suppression)
			}

			nextPeriod := services.AddCalendarDays(today, testCase.nextPeriodIn, time.UTC)
			ovulation := services.AddCalendarDays(today, testCase.ovulationIn, time.UTC)
			for name, pair := range map[string][2]*string{
				"next_period_start":      {payload.NextPeriodStart, new(nextPeriod.Format(statsOverviewDateLayout))},
				"ovulation_date":         {payload.OvulationDate, new(ovulation.Format(statsOverviewDateLayout))},
				"fertility_window_start": {payload.FertilityWindowStart, new(services.AddCalendarDays(ovulation, -5, time.UTC).Format(statsOverviewDateLayout))},
				"fertility_window_end":   {payload.FertilityWindowEnd, new(ovulation.Format(statsOverviewDateLayout))},
			} {
				if pair[0] == nil || *pair[0] != *pair[1] {
					t.Errorf("%s = %v, want %s", name, statsOverviewTextOrNull(pair[0]), *pair[1])
				}
			}

			// The rendered header names the same two days.
			slot, document := dashboardOvulationSlotText(t, app, authCookie)
			if want := services.LocalizedDateDisplay("en", ovulation); !strings.Contains(slot, want) {
				t.Errorf("dashboard ovulation slot = %q, want %q — the day ovulation_date names", slot, want)
			}
			if got, want := dashboardElementTextByDataAttr(t, document, "data-dashboard-next-period"), services.LocalizedDateDisplay("en", nextPeriod); !strings.Contains(got, want) {
				t.Errorf("dashboard next-period slot = %q, want %q — the day next_period_start names", got, want)
			}
		})
	}
}

func statsOverviewTextOrNull(value *string) string {
	if value == nil {
		return "null"
	}
	return *value
}
