package services

// confirmed_ovulation_verdict_test.go — the day the owner's temperatures
// confirmed is named by four surfaces inside the instance: the JSON overview
// (PublishedOverviewStats), the dashboard's ovulation line
// (BuildDashboardCycleContext), the calendar's solid marker and the stats BBT
// chart's marker (buildOwnerCurrentCycleBBTChart). Whether it may
// be named is decided in ONE place, the gate inside
// ConfirmedCurrentCycleOvulation (ConfirmedOvulationWithheld). The overview used
// to add a second condition — put the day back only under the fertility gate —
// which agreed with that owner only because ConfirmedOvulationWithheld happened
// to be a subset of FertilityProjectionSuppressed.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const confirmedVerdictDayKey = "2026-03-11"

type confirmedVerdictRow struct {
	signal   string
	suppress func(user *models.User, stats *CycleStats, today *time.Time)
	wantKept bool
}

// confirmedVerdictRows is one row per suppression signal plus the unsuppressed
// control, each applied to projectedWindowFixture (confirmed 2026-03-11).
func confirmedVerdictRows() []confirmedVerdictRow {
	return []confirmedVerdictRow{
		{signal: "", suppress: func(*models.User, *CycleStats, *time.Time) {}, wantKept: true},
		{signal: "DashboardPredictionDisabled", suppress: func(user *models.User, _ *CycleStats, _ *time.Time) { user.UnpredictableCycle = true }},
		{signal: "PregnancyPaused", suppress: func(_ *models.User, stats *CycleStats, _ *time.Time) { stats.PregnancyPaused = true }},
		{signal: "DashboardAwaitingFirstCycle", suppress: func(_ *models.User, stats *CycleStats, _ *time.Time) { stats.CompletedCycleCount = 0 }},
		{signal: "DashboardCycleOverdue", wantKept: true, suppress: func(_ *models.User, stats *CycleStats, today *time.Time) {
			// Cycle day 37 of a 28-day model: past 28 + 7.
			*today = AddCalendarDays(*today, 23, time.UTC)
			*stats = atToday(*stats, *today)
		}},
	}
}

func confirmedVerdictRowName(signal string) string {
	if signal == "" {
		return "no suppression"
	}
	return signal
}

// TestConfirmedCurrentCycleOvulationOwnsTheConfirmedDayVerdict pins the owner
// itself: for every signal the detector's answer is its gate's, and the rows
// cover every signal any of the three predicates disjoins — a signal added to
// ConfirmedOvulationWithheld alone demands a row as much as one added to either
// suppression predicate.
func TestConfirmedCurrentCycleOvulationOwnsTheConfirmedDayVerdict(t *testing.T) {
	covered := map[string]bool{}
	for _, testCase := range confirmedVerdictRows() {
		covered[testCase.signal] = true
		t.Run(confirmedVerdictRowName(testCase.signal), func(t *testing.T) {
			user, logs, stats, today := projectedWindowFixture(t)
			testCase.suppress(user, &stats, &today)

			if (testCase.signal != "") != FertilityProjectionSuppressed(user, stats) {
				t.Fatalf("fixture: fertility gate = %t for signal %q", FertilityProjectionSuppressed(user, stats), testCase.signal)
			}
			day, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC)
			if ok != testCase.wantKept || (ok && CalendarDayKey(day) != confirmedVerdictDayKey) {
				t.Fatalf("detector: confirmed %s (ok=%t), want ok=%t on %s", CalendarDayKey(day), ok, testCase.wantKept, confirmedVerdictDayKey)
			}
		})
	}

	root := predictionSuppressionRepoRoot(t)
	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(predictionSuppressionPredicateFile)))
	if err != nil {
		t.Fatalf("read %s: %v", predictionSuppressionPredicateFile, err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), predictionSuppressionPredicateFile, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", predictionSuppressionPredicateFile, err)
	}
	predicates := map[string]bool{"PredictionsSuppressed": true, "FertilityProjectionSuppressed": true, "ConfirmedOvulationWithheld": true}
	signals := 0
	for predicate := range predicates {
		for _, signal := range predictionSuppressionDisjunctsOf(t, file, predicate) {
			if predicates[signal] {
				continue
			}
			signals++
			if !covered[signal] {
				t.Errorf("%s disjoins %s, which has no row here: add one naming whether the confirmed day survives it", predicate, signal)
			}
		}
	}
	if signals == 0 {
		t.Fatal("the predicates disjoin no signal — the coverage check read nothing")
	}
}

// TestEverySuppressionSignalAgreesOnTheConfirmedDay asks each surface, for
// every row above, whether it names the confirmed day: all four read the one
// owner, so none may answer differently from the row.
func TestEverySuppressionSignalAgreesOnTheConfirmedDay(t *testing.T) {
	for _, testCase := range confirmedVerdictRows() {
		t.Run(confirmedVerdictRowName(testCase.signal), func(t *testing.T) {
			user, logs, stats, today := projectedWindowFixture(t)
			testCase.suppress(user, &stats, &today)

			published, _, apiConfirmed := PublishedOverviewStats(user, logs, stats, today, time.UTC)
			apiKept := apiConfirmed && CalendarDayKey(published.OvulationDate) == confirmedVerdictDayKey

			cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
			dashboardKept := cycleContext.DisplayOvulationConfirmed && CalendarDayKey(cycleContext.DisplayOvulationDate) == confirmedVerdictDayKey

			_, ovulation := calendarFertileDays(t, user, logs, stats, today)
			calendarKept := ovulation[confirmedVerdictDayKey]

			statsKept := statsBBTChartMarkerDayKey(stats, statsPageBBTChart(t, user, logs, stats, today)) == confirmedVerdictDayKey

			if apiKept != testCase.wantKept || dashboardKept != testCase.wantKept || calendarKept != testCase.wantKept || statsKept != testCase.wantKept {
				t.Fatalf("confirmed %s named: API=%t dashboard=%t calendar=%t stats chart=%t, want %t on all four",
					confirmedVerdictDayKey, apiKept, dashboardKept, calendarKept, statsKept, testCase.wantKept)
			}
		})
	}
}
