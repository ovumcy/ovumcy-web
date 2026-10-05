package services

import (
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-209: a regular owner with one or two completed cycles had an ovulation
// date and fertile window published everywhere. The floor is three completed
// cycles, the same number irregular mode needs. Each row below is read through
// the surfaces that carry the verdict: the resolver (and so the JSON overview's
// reason), the dashboard's timing frame, the hero's phase ribbon and the
// explainer; the grid, feed, webhook, banner and implantation hint are driven
// by TestFirstCycleFloorSuppressesFertilityOnEverySurface over the same tiers.

func awaitingMoreCyclesHistory(today time.Time, completed int) (*models.User, []models.DailyLog) {
	user := firstCycleFloorUser()
	starts := make([]time.Time, 0, completed+1)
	// The last start is 12 days ago; earlier ones are one 28-day cycle apart.
	for index := completed; index >= 0; index-- {
		starts = append(starts, today.AddDate(0, 0, -(12+28*index)))
	}
	return user, firstCycleFloorLogs(starts)
}

func TestAwaitingMoreCyclesIsOneVerdictAcrossTheDashboardSurfaces(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, location)
	today := DateAtLocation(now, location)

	for _, testCase := range []struct {
		name          string
		completed     int
		irregular     bool
		goal          string
		wantReasons   []SuppressionReason
		wantExplainer string
		wantFertility bool
	}{
		{name: "A1 regular trying, one cycle", completed: 1, goal: models.UsageGoalTrying, wantReasons: []SuppressionReason{SuppressionReasonAwaitingMoreCycles}, wantExplainer: "prediction.explainer.awaiting_more_cycles", wantFertility: true},
		{name: "A2 regular avoiding, two cycles", completed: 2, goal: models.UsageGoalAvoid, wantReasons: []SuppressionReason{SuppressionReasonAwaitingMoreCycles}, wantExplainer: "prediction.explainer.awaiting_more_cycles", wantFertility: true},
		{name: "A3 regular health, one cycle", completed: 1, goal: models.UsageGoalHealth, wantReasons: []SuppressionReason{SuppressionReasonAwaitingMoreCycles}, wantExplainer: "prediction.explainer.awaiting_more_cycles", wantFertility: true},
		{name: "control: three cycles publish", completed: 3, goal: models.UsageGoalTrying},
		{name: "control: zero cycles keep the first-cycle reason", completed: 0, goal: models.UsageGoalTrying, wantReasons: []SuppressionReason{SuppressionReasonAwaitingFirstCycle}, wantExplainer: "prediction.explainer.awaiting_first_cycle", wantFertility: true},
		{name: "control: irregular with two cycles keeps its own reason", completed: 2, irregular: true, goal: models.UsageGoalTrying, wantReasons: []SuppressionReason{SuppressionReasonIrregularNeedsData}, wantExplainer: "prediction.explainer.irregular_sparse", wantFertility: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			user, logs := awaitingMoreCyclesHistory(today, testCase.completed)
			user.UsageGoal = testCase.goal
			user.IrregularCycle = testCase.irregular
			stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(user, logs, now, location)
			if stats.CompletedCycleCount != testCase.completed {
				t.Fatalf("fixture: %d completed cycles, want %d", stats.CompletedCycleCount, testCase.completed)
			}

			verdict := ResolvePredictionSuppression(user, stats)
			if !slices.Equal(verdict.Reasons, testCase.wantReasons) {
				t.Fatalf("reasons = %v, want %v", verdict.Reasons, testCase.wantReasons)
			}
			if verdict.FertilitySuppressed != testCase.wantFertility {
				t.Fatalf("FertilitySuppressed = %v, want %v", verdict.FertilitySuppressed, testCase.wantFertility)
			}
			if testCase.wantFertility && testCase.completed > 0 && !testCase.irregular && verdict.PredictionsSuppressed {
				t.Fatal("the next-period estimate must stay in the one-or-two-cycles tier")
			}

			published, _ := PublishedStats(user, stats, logs, today, location)
			if got := !published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero(); got == testCase.wantFertility {
				t.Fatalf("published an ovulation date or window = %v with the fertility gate %v", got, testCase.wantFertility)
			}

			cycleContext := BuildDashboardCycleContext(user, logs, stats, today, location)
			if cycleContext.FertilitySuppressed != testCase.wantFertility {
				t.Fatalf("context FertilitySuppressed = %v, want %v", cycleContext.FertilitySuppressed, testCase.wantFertility)
			}
			frame := resolveDashboardTimingFrame(user, cycleContext, dashboardOwnerVisibility{})
			if frame.ShowOvulationEstimate == testCase.wantFertility {
				t.Fatalf("header ShowOvulationEstimate = %v for the %s goal with the gate %v", frame.ShowOvulationEstimate, testCase.goal, testCase.wantFertility)
			}
			if testCase.completed > 0 && frame.ShowFirstCycleBridge {
				t.Fatal("the first-cycle bridge line belongs to the zero-cycle tier only")
			}

			if got := BuildOwnerPredictionExplanation(user, cycleContext, false).PrimaryKey; got != testCase.wantExplainer {
				t.Fatalf("explainer = %q, want %q", got, testCase.wantExplainer)
			}
		})
	}
}
