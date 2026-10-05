package api

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestDashboardRibbonNamesNoOvulationBeforeThreeCompletedCycles is the render
// regression for the side finding of the one-or-two-cycles floor: a goal whose
// header hides the ovulation line (health, avoid) still printed "Cycle day 14:
// Ovulation" on the ribbon cell while the fertility half was withheld everywhere
// else. On cycle day 14 of a 28-day baseline, with one or two completed cycles,
// every ribbon cell must carry the withheld status and no cell may name the
// ovulation phase in its data attribute or its accessible name.
func TestDashboardRibbonNamesNoOvulationBeforeThreeCompletedCycles(t *testing.T) {
	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("load locales: %v", err)
	}
	ovulationLabel := manager.Messages("en")[services.PhaseTranslationKey("ovulation")]
	if strings.TrimSpace(ovulationLabel) == "" {
		t.Fatal("fixture: the English ovulation phase label must exist")
	}

	for name, testCase := range map[string]struct {
		goal            string
		completedCycles int
	}{
		"health, one completed cycle":    {goal: models.UsageGoalHealth, completedCycles: 1},
		"avoid, two completed cycles":    {goal: models.UsageGoalAvoid, completedCycles: 2},
		"trying, one completed cycle":    {goal: models.UsageGoalTrying, completedCycles: 1},
		"health, three completed cycles": {goal: models.UsageGoalHealth, completedCycles: 3},
	} {
		t.Run(name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "dashboard-ribbon-more-cycles@example.com", "StrongPass1", true)
			today := services.DateAtLocation(time.Now().UTC(), time.UTC)
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
				"usage_goal":        testCase.goal,
				"cycle_length":      28,
				"period_length":     5,
				"last_period_start": today.AddDate(0, 0, -13),
			}).Error; err != nil {
				t.Fatalf("seed cycle baseline: %v", err)
			}
			// completedCycles+1 recorded starts 28 days apart, the running cycle on day 14.
			for index := range testCase.completedCycles + 1 {
				if err := database.Create(&models.DailyLog{
					UserID:     user.ID,
					Date:       today.AddDate(0, 0, -13-28*index),
					IsPeriod:   true,
					CycleStart: true,
				}).Error; err != nil {
					t.Fatalf("seed cycle start %d: %v", index, err)
				}
			}

			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))

			cells := htmlFindElements(document, htmlNodeHasAttr("data-cycle-ribbon-day"))
			if len(cells) == 0 {
				t.Fatal("expected the dashboard ribbon to render its day cells")
			}
			published := testCase.completedCycles >= 3
			sawOvulation := false
			for _, cell := range cells {
				phase := htmlAttr(cell, "data-phase")
				label := htmlAttr(cell, "aria-label")
				if phase == "ovulation" || strings.Contains(label, ovulationLabel) {
					sawOvulation = true
					if !published {
						t.Errorf("ribbon day %s names ovulation (data-phase=%q, aria-label=%q) before three completed cycles", htmlAttr(cell, "data-cycle-ribbon-day"), phase, label)
					}
				}
				if !published && htmlAttr(cell, "data-cycle-ribbon-day") == "14" && phase != "withheld" {
					t.Errorf("ribbon day 14 carries phase %q, want withheld", phase)
				}
			}
			if published && !sawOvulation {
				t.Fatal("control: with three completed cycles the ribbon must name the ovulation day")
			}
		})
	}
}
