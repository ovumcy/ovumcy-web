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

			// The key and the summary restate the cells in words, so they
			// withhold exactly what the cells withhold.
			legendItems := htmlFindElements(document, htmlNodeHasAttr("data-cycle-ribbon-legend-item"))
			summaries := htmlFindElements(document, htmlNodeHasAttr("data-cycle-ribbon-summary"))
			if len(legendItems) == 0 || len(summaries) != 1 {
				t.Fatalf("expected the ribbon key and one summary, got %d items and %d summaries", len(legendItems), len(summaries))
			}
			keyNamesOvulation := false
			for _, item := range legendItems {
				if htmlAttr(item, "data-cycle-ribbon-legend-item") == "phase-ovulation" || strings.Contains(htmlNodeText(item), ovulationLabel) {
					keyNamesOvulation = true
				}
			}
			summaryNamesOvulation := strings.Contains(htmlNodeText(summaries[0]), ovulationLabel)
			if !published && (keyNamesOvulation || summaryNamesOvulation) {
				t.Errorf("ribbon key (%v) or summary (%v) names ovulation before three completed cycles", keyNamesOvulation, summaryNamesOvulation)
			}
			if published && !keyNamesOvulation {
				t.Error("control: with three completed cycles the ribbon key must list the ovulation swatch")
			}

			// The ring bends the same cells into a circle, so it withholds the
			// same: no ovulation arc and no fertile arc while the half is held back.
			ring := htmlFindElement(document, htmlNodeHasAttr("data-dashboard-cycle-ring"))
			if ring == nil || htmlFindElement(ring, htmlNodeHasAttr("data-cycle-ring-today")) == nil {
				t.Fatal("expected the ring with a today dot")
			}
			ringNamesOvulation := htmlFindElement(ring, htmlNodeAttrEquals("data-phase", "ovulation")) != nil
			ringShadesFertile := htmlFindElement(ring, htmlNodeHasAttr("data-cycle-ring-fertile")) != nil
			if !published && (ringNamesOvulation || ringShadesFertile) {
				t.Errorf("ring draws ovulation (%v) or the fertile arc (%v) before three completed cycles", ringNamesOvulation, ringShadesFertile)
			}
			if published && !(ringNamesOvulation && ringShadesFertile) {
				t.Error("control: with three completed cycles the ring must draw the ovulation and fertile arcs")
			}
		})
	}
}
