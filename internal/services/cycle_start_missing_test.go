package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestCycleStartMissingReadsTheResolvedStart(t *testing.T) {
	if !CycleStartMissing(CycleStats{}) {
		t.Fatal("expected a missing start to be reported for zero stats")
	}
	start := time.Date(2026, time.November, 3, 0, 0, 0, 0, time.UTC)
	if CycleStartMissing(CycleStats{LastPeriodStart: start}) {
		t.Fatal("expected a resolved start to clear the notice")
	}
}

// TestDashboardCycleContextGatesTheNoticeOnThePredictingBranch: the notice
// promises predictions, so only the branch that would predict carries it, and
// there it is the page's one explanation.
func TestDashboardCycleContextGatesTheNoticeOnThePredictingBranch(t *testing.T) {
	today := time.Date(2026, time.November, 27, 0, 0, 0, 0, time.UTC)
	owner := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5}

	context := BuildDashboardCycleContext(owner, nil, CycleStats{}, today, time.UTC)
	if !context.CycleStartMissing {
		t.Fatal("expected a start-less owner to carry the no-cycle-start notice")
	}
	if !context.DisplayNextPeriodStart.IsZero() {
		t.Fatalf("expected no next-period date without a start, got %s", context.DisplayNextPeriodStart)
	}
	if !context.AwaitingFirstCycle {
		t.Fatal("fixture no longer reaches the first-cycle tier, so the explainer check below is vacuous")
	}
	if key := BuildOwnerPredictionExplanation(owner, context, false).PrimaryKey; key != "" {
		t.Fatalf("expected the notice to stand alone, got explainer %q", key)
	}

	unpredictable := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, UnpredictableCycle: true}
	if BuildDashboardCycleContext(unpredictable, nil, CycleStats{}, today, time.UTC).CycleStartMissing {
		t.Fatal("expected prediction-off accounts to keep their own explainer, not a notice promising predictions")
	}

	paused := BuildDashboardCycleContext(owner, nil, CycleStats{PregnancyPaused: true}, today, time.UTC)
	if paused.CycleStartMissing {
		t.Fatal("expected the pregnancy pause to keep its own explainer")
	}

	start := today.AddDate(0, 0, -6)
	withStart := BuildDashboardCycleContext(owner, nil, CycleStats{LastPeriodStart: start, CurrentCycleDay: 7}, today, time.UTC)
	if withStart.CycleStartMissing {
		t.Fatal("expected a resolved start to clear the notice")
	}
}
