package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The dashboard is the surface the medical-safety floor is easiest to lose on,
// because it publishes the same claim three times: the gate the header reads
// (ShowFertilityStatus), the classification the status attribute carries, and
// the raw CycleStats copy the page hands to every partial below it. This file
// pins all three to the SHARED predicates rather than to any recombination of
// their disjuncts — the gate carried only the first-cycle floor, so an account
// in unpredictable-cycle mode, on a pregnancy pause, or past its own reference
// length read "Fertile window" beside the very notice saying its predictions
// are off.
//
// Each case names the tier and what it costs, and the unsuppressed row is the
// positive anchor: without it a guard that simply blanked every projection
// would read as green.

// dashboardSuppressionDay is the fixture's today. The window below is built
// around it so that the unsuppressed row genuinely classifies as fertile —
// a suppression assertion against a row that was never fertile proves nothing.
const dashboardSuppressionDay = "2026-03-09"

// dashboardSuppressionStats is a two-completed-cycle history whose projection
// puts today inside the fertile window: every field a suppressed tier must
// withhold is populated before the tier is applied.
func dashboardSuppressionStats(t *testing.T) CycleStats {
	t.Helper()
	return CycleStats{
		CompletedCycleCount:  2,
		CurrentCycleDay:      10,
		CurrentPhase:         "follicular",
		CurrentFertility:     FertilityStatusFertile,
		AverageCycleLength:   28,
		MedianCycleLength:    28,
		AveragePeriodLength:  5,
		LutealPhase:          14,
		LastPeriodStart:      mustParseDashboardServiceDay(t, "2026-02-28"),
		NextPeriodStart:      mustParseDashboardServiceDay(t, "2026-03-28"),
		OvulationDate:        mustParseDashboardServiceDay(t, "2026-03-13"),
		OvulationExact:       true,
		FertilityWindowStart: mustParseDashboardServiceDay(t, "2026-03-08"),
		FertilityWindowEnd:   mustParseDashboardServiceDay(t, "2026-03-13"),
	}
}

func TestBuildDashboardViewDataWithholdsEveryFertilityClaimTheSharedGateSuppresses(t *testing.T) {
	today := mustParseDashboardServiceDay(t, dashboardSuppressionDay)

	for name, testCase := range map[string]struct {
		mutateUser  func(*models.User)
		mutateStats func(*CycleStats)
		// wantFertility is the fertility half — the window, the ovulation date
		// and the classification the header declares.
		wantFertility bool
		// wantNextPeriod is the next-period half, which the first-cycle floor
		// deliberately does NOT withhold: its anchor is a recorded start and
		// only the length falls back to the onboarding slider.
		wantNextPeriod bool
	}{
		"nothing suppressed: the measured window is published": {
			wantFertility:  true,
			wantNextPeriod: true,
		},
		"unpredictable-cycle mode: the owner turned projections off": {
			mutateUser:     func(user *models.User) { user.UnpredictableCycle = true },
			wantNextPeriod: false,
		},
		"pregnancy pause: the latest fertility signal is a positive test": {
			mutateStats:    func(stats *CycleStats) { stats.PregnancyPaused = true },
			wantNextPeriod: false,
		},
		"cycle overdue past its own reference length": {
			mutateStats:    func(stats *CycleStats) { stats.CurrentCycleDay = 54 },
			wantNextPeriod: false,
		},
		"awaiting the first completed cycle": {
			mutateStats:    func(stats *CycleStats) { stats.CompletedCycleCount = 0 },
			wantNextPeriod: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			user := &models.User{ID: 11, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14, UsageGoal: models.UsageGoalAvoid}
			if testCase.mutateUser != nil {
				testCase.mutateUser(user)
			}
			stats := dashboardSuppressionStats(t)
			if testCase.mutateStats != nil {
				testCase.mutateStats(&stats)
			}
			// The fixture must be a live fertility claim before the tier is
			// applied, or every "withheld" assertion below is vacuous.
			if stats.CurrentFertility != FertilityStatusFertile {
				t.Fatalf("fixture: expected a fertile classification to suppress, got %q", stats.CurrentFertility)
			}

			service := NewDashboardViewService(
				&stubDashboardStatsProvider{stats: stats},
				&stubDashboardDayLogProvider{logEntry: models.DailyLog{Date: today}},
				&stubDashboardDayStateProvider{},
			)
			viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
			if err != nil {
				t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
			}

			// The gate is the shared predicate's answer, not a fourth
			// recombination of its disjuncts, and it is the same answer the
			// cycle context already resolved for the banner.
			if want := !FertilityProjectionSuppressed(user, stats); viewData.ShowFertilityStatus != want {
				t.Errorf("ShowFertilityStatus = %v, want the shared predicate's %v", viewData.ShowFertilityStatus, want)
			}
			if want := !viewData.CycleContext.FertilitySuppressed; viewData.ShowFertilityStatus != want {
				t.Errorf("ShowFertilityStatus = %v, want the context's already-resolved %v", viewData.ShowFertilityStatus, want)
			}
			if viewData.ShowFertilityStatus != testCase.wantFertility {
				t.Errorf("ShowFertilityStatus = %v, want %v for this tier", viewData.ShowFertilityStatus, testCase.wantFertility)
			}

			// The published copy carries the same decision, so a partial that
			// forgets the gate renders nothing instead of the raw claim.
			wantClassification := FertilityStatusUnknown
			if testCase.wantFertility {
				wantClassification = FertilityStatusFertile
			}
			if viewData.Stats.CurrentFertility != wantClassification {
				t.Errorf("published Stats.CurrentFertility = %q, want %q", viewData.Stats.CurrentFertility, wantClassification)
			}
			if got := !viewData.Stats.FertilityWindowStart.IsZero(); got != testCase.wantFertility {
				t.Errorf("published Stats.FertilityWindowStart present = %v, want %v", got, testCase.wantFertility)
			}
			if got := !viewData.Stats.FertilityWindowEnd.IsZero(); got != testCase.wantFertility {
				t.Errorf("published Stats.FertilityWindowEnd present = %v, want %v", got, testCase.wantFertility)
			}
			if got := !viewData.Stats.OvulationDate.IsZero(); got != testCase.wantFertility {
				t.Errorf("published Stats.OvulationDate present = %v, want %v", got, testCase.wantFertility)
			}
			if got := !viewData.Stats.NextPeriodStart.IsZero(); got != testCase.wantNextPeriod {
				t.Errorf("published Stats.NextPeriodStart present = %v, want %v", got, testCase.wantNextPeriod)
			}

			// RECORDED history is fact, not projection: no tier touches it.
			if viewData.Stats.CurrentCycleDay != stats.CurrentCycleDay {
				t.Errorf("published Stats.CurrentCycleDay = %d, want the recorded %d", viewData.Stats.CurrentCycleDay, stats.CurrentCycleDay)
			}
			if !viewData.Stats.LastPeriodStart.Equal(stats.LastPeriodStart) {
				t.Errorf("published Stats.LastPeriodStart = %v, want the recorded %v", viewData.Stats.LastPeriodStart, stats.LastPeriodStart)
			}
			if viewData.Stats.CompletedCycleCount != stats.CompletedCycleCount {
				t.Errorf("published Stats.CompletedCycleCount = %d, want the recorded %d", viewData.Stats.CompletedCycleCount, stats.CompletedCycleCount)
			}
		})
	}
}

// TestBuildDashboardCycleContextPausesTheNextPeriodEstimateForAnOverdueIrregularAccount
// pins the branch ORDER inside buildDashboardPredictionDisplay. The overdue
// signal reached NextPeriodEstimatePaused only when no earlier branch claimed
// the display first, and dashboardNeedsNextPeriodData claimed it for exactly the
// cohort with the least evidence: an irregular account with fewer than three
// completed cycles. That account read a named date carrying a "needs more
// cycles" qualifier while overdue — a qualifier where the floor is suppression.
//
// The not-overdue row is the positive anchor: the needs-data message is a real
// state and must survive, so the fix may not be "pause everything".
func TestBuildDashboardCycleContextPausesTheNextPeriodEstimateForAnOverdueIrregularAccount(t *testing.T) {
	today := mustParseDashboardServiceDay(t, dashboardSuppressionDay)

	for name, testCase := range map[string]struct {
		currentCycleDay int
		wantOverdue     bool
	}{
		"overdue past the reference length": {currentCycleDay: 54, wantOverdue: true},
		"inside the reference length":       {currentCycleDay: 10, wantOverdue: false},
	} {
		t.Run(name, func(t *testing.T) {
			user := &models.User{ID: 12, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14, IrregularCycle: true}
			stats := dashboardSuppressionStats(t)
			stats.CurrentCycleDay = testCase.currentCycleDay

			// Fixture invariants: this is the exact collision — the thin-history
			// branch is armed AND the overdue signal is set.
			if !dashboardNeedsNextPeriodData(user, stats, stats.NextPeriodStart) {
				t.Fatal("fixture: expected the thin-history next-period branch to be armed")
			}
			if got := DashboardCycleOverdue(user, stats); got != testCase.wantOverdue {
				t.Fatalf("fixture: DashboardCycleOverdue = %v, want %v", got, testCase.wantOverdue)
			}

			cycleContext := BuildDashboardCycleContext(user, stats, today, time.UTC)
			if cycleContext.NextPeriodEstimatePaused != testCase.wantOverdue {
				t.Errorf("NextPeriodEstimatePaused = %v, want %v", cycleContext.NextPeriodEstimatePaused, testCase.wantOverdue)
			}
			if got := !cycleContext.DisplayNextPeriodStart.IsZero(); got == testCase.wantOverdue {
				t.Errorf("DisplayNextPeriodStart present = %v while overdue = %v; an overdue cycle may name no date", got, testCase.wantOverdue)
			}
			if got := cycleContext.DisplayNextPeriodNeedsData; got == testCase.wantOverdue {
				t.Errorf("DisplayNextPeriodNeedsData = %v while overdue = %v; the qualifier may not stand in for suppression", got, testCase.wantOverdue)
			}
		})
	}
}
