package api

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestDashboardRibbonTodayCellWearsTheHeaderPhase: the header names today's
// phase and the "today" marker points at a ribbon cell; the two are one answer.
// On the days the published ovulation had rolled, the header said "luteal"
// while the marker stood on the ribbon's "ovulation" cell (WEB-500).
func TestDashboardRibbonTodayCellWearsTheHeaderPhase(t *testing.T) {
	// The days around ovulation are where the average-first and median-first
	// lengths part; these are the ones that disagreed before the fix, plus an
	// even history as the control. A full 1..27 sweep cost two minutes of the
	// api package for the same answer.
	for _, testCase := range []struct {
		history []int
		days    []int
	}{
		{history: []int{31, 29, 29}, days: []int{14, 15, 16, 17}},
		{history: []int{26, 30, 27}, days: []int{12, 13, 14, 15}},
		{history: []int{28, 28, 28}, days: []int{14, 15}},
	} {
		history := testCase.history
		for _, cycleDay := range testCase.days {
			t.Run(fmt.Sprintf("history %v, cycle day %d", history, cycleDay), func(t *testing.T) {
				app, database := newOnboardingTestApp(t)
				user := createOnboardingTestUser(t, database, "dashboard-ribbon-today-phase@example.com", "StrongPass1", true)
				today := services.DateAtLocation(time.Now().UTC(), time.UTC)
				runningStart := today.AddDate(0, 0, -(cycleDay - 1))
				if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
					"usage_goal":        models.UsageGoalHealth,
					"cycle_length":      28,
					"period_length":     5,
					"last_period_start": runningStart,
				}).Error; err != nil {
					t.Fatalf("seed cycle baseline: %v", err)
				}
				start := runningStart
				starts := []time.Time{start}
				for index := len(history) - 1; index >= 0; index-- {
					start = start.AddDate(0, 0, -history[index])
					starts = append(starts, start)
				}
				for _, day := range starts {
					if err := database.Create(&models.DailyLog{UserID: user.ID, Date: day, IsPeriod: true, CycleStart: true}).Error; err != nil {
						t.Fatalf("seed cycle start: %v", err)
					}
				}

				authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
				document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))

				header := htmlFindElement(document, htmlNodeHasAttr("data-dashboard-status-header"))
				todayCell := htmlFindElement(document, func(node *html.Node) bool {
					return htmlHasAttr(node, "data-cycle-ribbon-day") && htmlAttr(node, "data-today") == "true"
				})
				if header == nil || todayCell == nil {
					t.Fatal("fixture: the ribbon must draw today's cell")
				}
				headerPhase := htmlAttr(header, "data-dashboard-phase")
				cellPhase := htmlAttr(todayCell, "data-phase")
				if headerPhase != cellPhase {
					t.Fatalf("header names %q but today's ribbon cell is %q", headerPhase, cellPhase)
				}
			})
		}
	}
}
