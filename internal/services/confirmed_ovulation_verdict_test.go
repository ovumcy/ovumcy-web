package services

// confirmed_ovulation_verdict_test.go — the day the owner's temperatures
// confirmed is named by three surfaces inside the instance: the JSON overview
// (PublishedOverviewStats), the dashboard's ovulation line
// (BuildDashboardCycleContext) and the calendar's solid marker. All three read
// ONE decision, PredictionSuppression.KeepConfirmedOvulation. The overview used
// to put the day back under ANY fertility gate, which matched the other two only
// because ConfirmedOvulationWithheld happened to be a subset of
// FertilityProjectionSuppressed.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestEverySuppressionSignalAgreesOnTheConfirmedDay walks every signal either
// predicate disjoins, plus the unsuppressed control, and asks each surface
// whether it names the confirmed 2026-03-11. The rows are checked against the
// predicates' own disjuncts, so a signal added to either one without a row here
// fails instead of passing unexamined.
func TestEverySuppressionSignalAgreesOnTheConfirmedDay(t *testing.T) {
	const confirmedKey = "2026-03-11"
	type row struct {
		signal   string
		suppress func(user *models.User, stats *CycleStats, today *time.Time)
		wantKept bool
	}
	rows := []row{
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

	covered := map[string]bool{}
	for _, testCase := range rows {
		covered[testCase.signal] = true
		name := testCase.signal
		if name == "" {
			name = "no suppression"
		}
		t.Run(name, func(t *testing.T) {
			user, logs, stats, today := projectedWindowFixture(t)
			testCase.suppress(user, &stats, &today)

			verdict := ResolvePredictionSuppression(user, stats)
			if (testCase.signal != "") != verdict.FertilitySuppressed {
				t.Fatalf("fixture: fertility gate = %t for signal %q", verdict.FertilitySuppressed, testCase.signal)
			}
			if verdict.ConfirmedOvulationWithheld == testCase.wantKept {
				t.Fatalf("verdict: ConfirmedOvulationWithheld = %t, want %t", verdict.ConfirmedOvulationWithheld, !testCase.wantKept)
			}

			published, _, apiConfirmed := PublishedOverviewStats(user, logs, stats, today, time.UTC)
			apiKept := apiConfirmed && CalendarDayKey(published.OvulationDate) == confirmedKey

			cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
			dashboardKept := cycleContext.DisplayOvulationConfirmed && CalendarDayKey(cycleContext.DisplayOvulationDate) == confirmedKey

			_, ovulation := calendarFertileDays(t, user, logs, stats, today)
			calendarKept := ovulation[confirmedKey]

			if apiKept != testCase.wantKept || dashboardKept != testCase.wantKept || calendarKept != testCase.wantKept {
				t.Fatalf("confirmed %s named: API=%t dashboard=%t calendar=%t, want %t on all three",
					confirmedKey, apiKept, dashboardKept, calendarKept, testCase.wantKept)
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
	predicates := map[string]bool{"PredictionsSuppressed": true, "FertilityProjectionSuppressed": true}
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

// TestKeepConfirmedOvulationFollowsTheWithheldVerdictNotTheFertilityGate
// covers the verdicts the tree cannot produce today: a fertility-only signal
// that withholds the confirmed day and one that keeps it. All three surfaces
// read the day through this method, so its answer is theirs.
func TestKeepConfirmedOvulationFollowsTheWithheldVerdictNotTheFertilityGate(t *testing.T) {
	day := time.Date(2026, time.March, 11, 0, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name     string
		verdict  PredictionSuppression
		ok       bool
		wantKept bool
	}{
		{name: "no suppression", verdict: PredictionSuppression{}, ok: true, wantKept: true},
		{name: "overdue shape: both gates, day kept", verdict: PredictionSuppression{PredictionsSuppressed: true, FertilitySuppressed: true}, ok: true, wantKept: true},
		{name: "hypothetical fertility-only signal that withholds the day", verdict: PredictionSuppression{FertilitySuppressed: true, ConfirmedOvulationWithheld: true}, ok: true, wantKept: false},
		{name: "hypothetical fertility-only signal that keeps the day", verdict: PredictionSuppression{FertilitySuppressed: true}, ok: true, wantKept: true},
		{name: "nothing confirmed", verdict: PredictionSuppression{}, ok: false, wantKept: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, kept := testCase.verdict.KeepConfirmedOvulation(day, testCase.ok)
			if kept != testCase.wantKept {
				t.Fatalf("kept = %t, want %t for %+v", kept, testCase.wantKept, testCase.verdict)
			}
			if kept && !got.Equal(day) {
				t.Fatalf("kept day = %s, want %s", CalendarDayKey(got), CalendarDayKey(day))
			}
			if !kept && !got.IsZero() {
				t.Fatalf("a withheld verdict returned a day: %s", CalendarDayKey(got))
			}
		})
	}
}
