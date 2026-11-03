package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The "Date is already in the past." notice beside "Oct 29 - Nov 2" on Nov 2
// read the window as gone while its last day was today (WEB-448).
func TestBuildDashboardCycleContextReadsThePeriodDaysAsPastOnlyAfterTheirLastDay(t *testing.T) {
	userStart := mustParseDashboardDay(t, "2026-02-10")
	user := &models.User{CycleLength: 28, PeriodLength: 5, LastPeriodStart: &userStart}
	stats := CycleStats{
		LastPeriodStart:     userStart,
		MedianCycleLength:   28,
		AveragePeriodLength: 5,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-03-10"),
		OvulationDate:       mustParseDashboardDay(t, "2026-02-24"),
	}
	for name, testCase := range map[string]struct {
		today    string
		cycleDay int
		wantPast bool
	}{
		"inside the predicted period days": {today: "2026-03-12", cycleDay: 31, wantPast: false},
		"on the last predicted period day": {today: "2026-03-14", cycleDay: 33, wantPast: false},
		"the day after the last one":       {today: "2026-03-15", cycleDay: 34, wantPast: true},
	} {
		t.Run(name, func(t *testing.T) {
			stats := stats
			stats.CurrentCycleDay = testCase.cycleDay
			cycleContext := BuildDashboardCycleContext(user, nil, stats, mustParseDashboardDay(t, testCase.today), time.UTC)
			if cycleContext.DisplayNextPeriodUseRange || cycleContext.DisplayNextPeriodEnd.Format("2006-01-02") != "2026-03-14" {
				t.Fatalf("expected the single-date period days ending 2026-03-14, got range=%v end=%s", cycleContext.DisplayNextPeriodUseRange, cycleContext.DisplayNextPeriodEnd.Format("2006-01-02"))
			}
			if cycleContext.NextPeriodInPast != testCase.wantPast {
				t.Fatalf("expected NextPeriodInPast=%v, got %v", testCase.wantPast, cycleContext.NextPeriodInPast)
			}
		})
	}
}

// The pause explainer told the owner to use Remove result on a test the
// dashboard form cannot reach; it now names the test day (WEB-449).
func TestBuildOwnerPredictionExplanationDatesThePregnancyPause(t *testing.T) {
	owner := &models.User{Role: models.RoleOwner}
	testDay := mustParseDashboardDay(t, "2026-03-05")

	explanation := BuildOwnerPredictionExplanation(owner, DashboardCycleContext{PredictionDisabled: true, PregnancyPaused: true, PregnancyTestDate: testDay}, false)
	if explanation.PrimaryKey != predictionExplainerPregnancyPausedKey || !explanation.PrimaryDate.Equal(testDay) {
		t.Fatalf("expected the pause sentence dated %s, got %#v", testDay.Format("2006-01-02"), explanation)
	}

	explanation = BuildOwnerPredictionExplanation(owner, DashboardCycleContext{AwaitingFirstCycle: true, PregnancyTestDate: testDay}, false)
	if !explanation.PrimaryDate.IsZero() {
		t.Fatalf("expected no date on a sentence other than the pause, got %#v", explanation)
	}
}

func TestBuildCycleStatsFromLogsCarriesThePausingTestDate(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5}
	testDay := mustParseDashboardDay(t, "2026-03-05")
	logs := []models.DailyLog{
		{Date: mustParseDashboardDay(t, "2026-02-10"), IsPeriod: true, CycleStart: true},
		{Date: testDay, PregnancyTest: models.PregnancyTestPositive},
	}
	stats := BuildCycleStatsFromLogs(user, logs, mustParseDashboardDay(t, "2026-03-10"), time.UTC)
	if !stats.PregnancyPaused || !stats.PregnancyTestDate.Equal(testDay) {
		t.Fatalf("expected the pause held on %s, got paused=%v date=%s", testDay.Format("2006-01-02"), stats.PregnancyPaused, stats.PregnancyTestDate.Format("2006-01-02"))
	}
	if context := BuildDashboardCycleContext(user, logs, stats, mustParseDashboardDay(t, "2026-03-10"), time.UTC); !context.PregnancyTestDate.Equal(testDay) {
		t.Fatalf("expected the context to carry the test day, got %s", context.PregnancyTestDate.Format("2006-01-02"))
	}
}

// The hero showed Menstrual with the fertility half withheld and no reason
// (WEB-461). The completed-cycle floors keep their own sentence.
func TestBuildOwnerPredictionExplanationNamesBleedingOutsideTheCycleStart(t *testing.T) {
	owner := &models.User{Role: models.RoleOwner}
	for name, testCase := range map[string]struct {
		context DashboardCycleContext
		wantKey string
	}{
		"floors cleared":        {context: DashboardCycleContext{BleedingOutsideCycleStart: true}, wantKey: predictionExplainerBleedingOutsideKey},
		"first-cycle floor":     {context: DashboardCycleContext{BleedingOutsideCycleStart: true, AwaitingFirstCycle: true}, wantKey: predictionExplainerAwaitingFirstCycleKey},
		"more-cycles floor":     {context: DashboardCycleContext{BleedingOutsideCycleStart: true, AwaitingMoreCycles: true}, wantKey: predictionExplainerAwaitingMoreCyclesKey},
		"pregnancy pause first": {context: DashboardCycleContext{BleedingOutsideCycleStart: true, PredictionDisabled: true, PregnancyPaused: true}, wantKey: predictionExplainerPregnancyPausedKey},
	} {
		t.Run(name, func(t *testing.T) {
			if got := BuildOwnerPredictionExplanation(owner, testCase.context, false).PrimaryKey; got != testCase.wantKey {
				t.Fatalf("expected %q, got %q", testCase.wantKey, got)
			}
		})
	}

	stats := CycleStats{CompletedCycleCount: 4, MedianCycleLength: 28, CurrentCycleDay: 12, BleedingOutsideCycleStart: true}
	if !BuildDashboardCycleContext(&models.User{CycleLength: 28}, nil, stats, mustParseDashboardDay(t, "2026-03-10"), time.UTC).BleedingOutsideCycleStart {
		t.Fatalf("expected the context to carry the bleeding-outside signal")
	}
}

// WEB-450: a first-cycle account trying to conceive read the same sentence in
// the bridge and in the explainer. WEB-426: the badge for a recorded egg-white
// observation names the observation before the first completed cycle. WEB-471:
// and in every other state that withholds fertility yet still shows the badge —
// spotting today on a 3+ cycle history read "Best timing: fertile window
// (estimate)" beside "Fertile details held back". The states that withhold the
// badge itself are TestBuildDashboardViewDataWithholdsHighFertilityBadgeWhileFertilitySuppressed.
func TestBuildDashboardViewDataTellsTheFirstCycleOnce(t *testing.T) {
	today := mustParseDashboardServiceDay(t, "2026-02-21")
	for name, testCase := range map[string]struct {
		completed       int
		bleedingOutside bool
		wantObserved    bool
	}{
		"no completed cycle":     {completed: 0, wantObserved: true},
		"three completed cycles": {completed: 3, wantObserved: false},
		"bleeding_outside_cycle_start after three completed cycles":     {completed: 3, bleedingOutside: true, wantObserved: true},
		"bleeding_outside_cycle_start before the first completed cycle": {completed: 0, bleedingOutside: true, wantObserved: true},
	} {
		t.Run(name, func(t *testing.T) {
			user := &models.User{ID: 9, Role: models.RoleOwner, CycleLength: 28, UsageGoal: models.UsageGoalTrying, TrackBBT: true}
			service := NewDashboardViewService(
				&stubDashboardStatsProvider{stats: CycleStats{
					CompletedCycleCount:       testCase.completed,
					BleedingOutsideCycleStart: testCase.bleedingOutside,
					CurrentCycleDay:           5,
					MedianCycleLength:         28,
					LastPeriodStart:           mustParseDashboardServiceDay(t, "2026-02-17"),
					NextPeriodStart:           mustParseDashboardServiceDay(t, "2026-03-17"),
				}},
				&stubDashboardViewerProvider{logEntry: models.DailyLog{Date: today, CervicalMucus: models.CervicalMucusEggWhite}},
				&stubDashboardDayStateProvider{},
			)
			viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
			if err != nil {
				t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
			}
			if viewData.ShowFirstCycleBridge {
				t.Fatalf("expected no bridge line beside the explainer, explainer=%q", viewData.PredictionExplanationPrimaryKey)
			}
			if !viewData.ShowHighFertilityBadge {
				t.Fatalf("expected the egg-white badge to show")
			}
			if viewData.CycleContext.FertilitySuppressed != testCase.wantObserved || viewData.ShowFertilityStatus == testCase.wantObserved {
				t.Fatalf("fixture: fertility suppressed=%v, header status shown=%v; want the badge beside a withheld header exactly when observed=%v",
					viewData.CycleContext.FertilitySuppressed, viewData.ShowFertilityStatus, testCase.wantObserved)
			}
			if viewData.HighFertilityBadgeObserved != testCase.wantObserved {
				t.Fatalf("expected observation wording=%v, got %v", testCase.wantObserved, viewData.HighFertilityBadgeObserved)
			}
		})
	}
}
