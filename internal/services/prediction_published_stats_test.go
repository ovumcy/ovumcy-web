package services

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// publishedStatsAnchor is a fixed day, so a case reads the same in January and
// on a leap day.
var publishedStatsAnchor = time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

// publishedStatsBase is an owner whose projection is fully calculable and
// unsuppressed: three completed cycles, a recorded anchor, every forward-looking
// field filled. Each case below turns on exactly ONE signal against it, so a
// cleared field is attributable to that signal and to nothing else.
func publishedStatsBase() CycleStats {
	return CycleStats{
		CurrentCycleDay:      6,
		CurrentPhase:         "follicular",
		CurrentFertility:     FertilityStatusFertile,
		AverageCycleLength:   28,
		MedianCycleLength:    28,
		MinCycleLength:       27,
		MaxCycleLength:       29,
		CompletedCycleCount:  3,
		AveragePeriodLength:  5,
		LastCycleLength:      28,
		LastPeriodLength:     5,
		LutealPhase:          14,
		LastPeriodStart:      publishedStatsAnchor,
		NextPeriodStart:      publishedStatsAnchor.AddDate(0, 0, 28),
		OvulationDate:        publishedStatsAnchor.AddDate(0, 0, 14),
		OvulationExact:       true,
		FertilityWindowStart: publishedStatsAnchor.AddDate(0, 0, 9),
		FertilityWindowEnd:   publishedStatsAnchor.AddDate(0, 0, 15),
	}
}

// publishedStatsCase is one suppression signal, the state that turns it on, and
// what the payload must then say. The signal name is the identifier the
// predicates disjoin, which is what ties the table to the tree.
type publishedStatsCase struct {
	name              string
	signal            string
	reason            SuppressionReason
	user              *models.User
	stats             CycleStats
	wantPredictions   bool
	wantFertility     bool
	wantNextPeriodSet bool
}

func publishedStatsCases() []publishedStatsCase {
	overdue := publishedStatsBase()
	// Past the account's own reference length (28) by more than a week.
	overdue.CurrentCycleDay = 40

	paused := publishedStatsBase()
	paused.PregnancyPaused = true

	firstCycle := publishedStatsBase()
	firstCycle.CompletedCycleCount = 0

	return []publishedStatsCase{
		{
			name:            "unpredictable cycle mode",
			signal:          "DashboardPredictionDisabled",
			reason:          SuppressionReasonUnpredictableCycle,
			user:            &models.User{UnpredictableCycle: true},
			stats:           publishedStatsBase(),
			wantPredictions: true,
			wantFertility:   true,
		},
		{
			name:            "pregnancy pause",
			signal:          "PregnancyPaused",
			reason:          SuppressionReasonPregnancyPause,
			user:            &models.User{},
			stats:           paused,
			wantPredictions: true,
			wantFertility:   true,
		},
		{
			name:            "cycle overdue past its own reference length",
			signal:          "DashboardCycleOverdue",
			reason:          SuppressionReasonCycleOverdue,
			user:            &models.User{},
			stats:           overdue,
			wantPredictions: true,
			wantFertility:   true,
		},
		{
			// The fertility-only tier: the projected next period is anchored on a
			// recorded start and stays, which is the one case where the two gates
			// disagree — and the one a single boolean would have collapsed.
			name:              "zero completed cycles",
			signal:            "DashboardAwaitingFirstCycle",
			reason:            SuppressionReasonAwaitingFirstCycle,
			user:              &models.User{},
			stats:             firstCycle,
			wantPredictions:   false,
			wantFertility:     true,
			wantNextPeriodSet: true,
		},
	}
}

// TestEverySuppressionSignalHasAPublishedReason ties the reason vocabulary to
// the predicates themselves.
//
// SuppressionReason is a SECOND spelling of what PredictionsSuppressed and
// FertilityProjectionSuppressed disjoin, and a second spelling nothing compares
// is how the two drift: a fifth signal added to a predicate would suppress the
// payload while naming no reason at all, so a client would read dates withheld
// for no stated cause — which is worse than the qualifier the invariant already
// refuses. The predicate file is parsed rather than the reasons re-listed by
// hand, so the check moves with the tree.
func TestEverySuppressionSignalHasAPublishedReason(t *testing.T) {
	root := predictionSuppressionRepoRoot(t)
	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(predictionSuppressionPredicateFile)))
	if err != nil {
		t.Fatalf("read %s: %v", predictionSuppressionPredicateFile, err)
	}
	fileSet := token.NewFileSet()
	file, parseErr := parser.ParseFile(fileSet, predictionSuppressionPredicateFile, source, 0)
	if parseErr != nil {
		t.Fatalf("parse %s: %v", predictionSuppressionPredicateFile, parseErr)
	}

	// A predicate disjoined by the other one is composition, not a signal.
	predicateNames := map[string]bool{
		"PredictionsSuppressed":         true,
		"FertilityProjectionSuppressed": true,
	}

	disjoined := map[string]bool{}
	for predicate := range predicateNames {
		for _, name := range predictionSuppressionDisjunctsOf(t, file, predicate) {
			if !predicateNames[name] {
				disjoined[name] = true
			}
		}
	}
	if len(disjoined) == 0 {
		t.Fatal("the predicates disjoin no signal — this check is about a tree nobody read")
	}

	covered := map[string]bool{}
	for _, testCase := range publishedStatsCases() {
		covered[testCase.signal] = true
	}

	for name := range disjoined {
		if !covered[name] {
			t.Fatalf("%s is disjoined by a suppression predicate and no case here turns it on — a payload suppressed by it would name no reason. Add the signal's SuppressionReason and its case", name)
		}
	}
	// Both directions: a case naming a signal the predicates dropped is a reason
	// published for a state that can no longer occur.
	for name := range covered {
		if !disjoined[name] {
			t.Fatalf("the case table names %q, which neither predicate disjoins any more — drop the case and its SuppressionReason rather than publishing a reason nothing can produce", name)
		}
	}
}

// TestResolvePredictionSuppressionNamesTheSignalThatHeld runs each signal's own
// state through the resolver: the verdict must match the predicates AND name
// exactly the reason that state turned on. Exactly one, because each case moves
// one field off the unsuppressed base — a second reason would mean the base is
// not the clean state this table claims.
func TestResolvePredictionSuppressionNamesTheSignalThatHeld(t *testing.T) {
	for _, testCase := range publishedStatsCases() {
		t.Run(testCase.name, func(t *testing.T) {
			verdict := ResolvePredictionSuppression(testCase.user, testCase.stats)

			if verdict.PredictionsSuppressed != testCase.wantPredictions {
				t.Fatalf("PredictionsSuppressed = %v, want %v", verdict.PredictionsSuppressed, testCase.wantPredictions)
			}
			if verdict.FertilitySuppressed != testCase.wantFertility {
				t.Fatalf("FertilitySuppressed = %v, want %v", verdict.FertilitySuppressed, testCase.wantFertility)
			}
			if len(verdict.Reasons) != 1 || verdict.Reasons[0] != testCase.reason {
				t.Fatalf("reasons = %v, want exactly [%s]", verdict.Reasons, testCase.reason)
			}
		})
	}
}

// TestPublishedStatsClearsEveryDateItsVerdictRefuses is the clearing half: the
// gate and the data behind it travel together, so a surface that forgets the
// verdict has nothing to publish rather than a suppressed estimate.
func TestPublishedStatsClearsEveryDateItsVerdictRefuses(t *testing.T) {
	for _, testCase := range publishedStatsCases() {
		t.Run(testCase.name, func(t *testing.T) {
			published, verdict := PublishedStats(testCase.user, testCase.stats)

			if !published.OvulationDate.IsZero() {
				t.Fatalf("published an ovulation date under %s", verdict.Reasons)
			}
			if published.OvulationExact {
				t.Fatalf("published the ovulation qualifier under %s", verdict.Reasons)
			}
			if !published.FertilityWindowStart.IsZero() || !published.FertilityWindowEnd.IsZero() {
				t.Fatalf("published a fertile window under %s", verdict.Reasons)
			}
			if published.CurrentFertility != FertilityStatusUnknown {
				t.Fatalf("published fertility %q under %s", published.CurrentFertility, verdict.Reasons)
			}
			if published.NextPeriodStart.IsZero() == testCase.wantNextPeriodSet {
				t.Fatalf("next period start present = %v, want %v under %s", !published.NextPeriodStart.IsZero(), testCase.wantNextPeriodSet, verdict.Reasons)
			}

			// Recorded history is fact, not projection, and survives every tier.
			if !published.LastPeriodStart.Equal(testCase.stats.LastPeriodStart) {
				t.Fatalf("cleared the recorded last period start under %s", verdict.Reasons)
			}
			if published.CurrentCycleDay != testCase.stats.CurrentCycleDay ||
				published.MedianCycleLength != testCase.stats.MedianCycleLength ||
				published.CompletedCycleCount != testCase.stats.CompletedCycleCount {
				t.Fatalf("cleared recorded history under %s", verdict.Reasons)
			}
		})
	}
}

// TestPublishedStatsDropsAPhaseWhoseOnlySourceIsTheClearedProjection covers the
// label half of the clearing. Three of the five phases are today's position
// relative to the projected ovulation date, so publishing one beside a cleared
// date states the withheld claim in a word.
//
// The three cases are the whole rule: a projection-derived phase goes, a
// recorded one stays, and the fertility-only tier clears it too — the day those
// labels are read off is exactly what that floor withholds.
func TestPublishedStatsDropsAPhaseWhoseOnlySourceIsTheClearedProjection(t *testing.T) {
	paused := func(phase string) CycleStats {
		stats := publishedStatsBase()
		stats.PregnancyPaused = true
		stats.CurrentPhase = phase
		return stats
	}
	firstCycle := publishedStatsBase()
	firstCycle.CompletedCycleCount = 0
	firstCycle.CurrentPhase = cyclePhaseFollicular

	for _, testCase := range []struct {
		name  string
		stats CycleStats
		want  string
	}{
		{name: "projected phase under the whole-projection gate", stats: paused(cyclePhaseOvulation), want: cyclePhaseUnknown},
		{name: "recorded phase under the whole-projection gate", stats: paused(cyclePhaseMenstrual), want: cyclePhaseMenstrual},
		{name: "projected phase under the first-cycle floor", stats: firstCycle, want: cyclePhaseUnknown},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			published, _ := PublishedStats(&models.User{}, testCase.stats)

			if published.CurrentPhase != testCase.want {
				t.Fatalf("current phase = %q, want %q", published.CurrentPhase, testCase.want)
			}
		})
	}
}

// TestPublishedStatsLeavesAnUnsuppressedProjectionWhole pins the other end: with
// no signal holding, the adapter is the identity and the verdict names nothing.
// Without it, a clearing rule that fired unconditionally would pass every
// assertion above.
func TestPublishedStatsLeavesAnUnsuppressedProjectionWhole(t *testing.T) {
	stats := publishedStatsBase()

	published, verdict := PublishedStats(&models.User{}, stats)

	if verdict.PredictionsSuppressed || verdict.FertilitySuppressed {
		t.Fatalf("an unsuppressed owner got verdict %+v", verdict)
	}
	if len(verdict.Reasons) != 0 {
		t.Fatalf("an unsuppressed owner got reasons %v", verdict.Reasons)
	}
	if published != stats {
		t.Fatal("the adapter changed an unsuppressed projection")
	}
}

// TestSuppressionReasonsAreDistinctWireValues guards the vocabulary itself: two
// reasons sharing a string would let a client branch on one state and silently
// act on another.
func TestSuppressionReasonsAreDistinctWireValues(t *testing.T) {
	seen := map[SuppressionReason]string{}
	for _, testCase := range publishedStatsCases() {
		if previous, duplicate := seen[testCase.reason]; duplicate {
			t.Fatalf("%q and %q publish the same reason %q", previous, testCase.name, testCase.reason)
		}
		if testCase.reason == "" {
			t.Fatalf("%q publishes an empty reason", testCase.name)
		}
		seen[testCase.reason] = testCase.name
	}

	spellings := make([]string, 0, len(seen))
	for reason := range seen {
		spellings = append(spellings, string(reason))
	}
	sort.Strings(spellings)
	t.Logf("published suppression reasons: %v", spellings)
}
