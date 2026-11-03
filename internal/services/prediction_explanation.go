package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// PrimaryCount is the number the primary sentence names through its one %d,
// and also what selects its plural form; 0 means the sentence takes no number.
// It is resolved here once, so no page decides per key whether to format.
// PrimaryDate is the day the pregnancy-pause sentence names and links to the
// calendar, where its "Remove result" lives; zero for every other sentence.
type PredictionExplanation struct {
	PrimaryKey   string
	PrimaryCount int
	PrimaryDate  time.Time
	SecondaryKey string
}

// The explainer keys whose sentence names the completed-cycle floor.
const (
	predictionExplainerIrregularSparseKey    = "prediction.explainer.irregular_sparse"
	predictionExplainerAwaitingMoreCyclesKey = "prediction.explainer.awaiting_more_cycles"
)

const (
	predictionExplainerPregnancyPausedKey    = "prediction.explainer.pregnancy_paused"
	predictionExplainerAwaitingFirstCycleKey = "prediction.explainer.awaiting_first_cycle"
	predictionExplainerBleedingOutsideKey    = "prediction.explainer.bleeding_outside_cycle_start"
)

// predictionExplanationPrimaryCount is the completed-cycle floor for a sentence
// that names it, 0 for one that takes no number.
func predictionExplanationPrimaryCount(primaryKey string) int {
	switch primaryKey {
	case predictionExplainerIrregularSparseKey, predictionExplainerAwaitingMoreCyclesKey:
		return FertilityMinimumCycles()
	default:
		return 0
	}
}

// withoutExplanationRestatedByLateCycle drops the awaiting-more-cycles explainer
// when the late-cycle card above it already says the same thing. A cycle that
// runs late on one or two completed cycles has no personal range yet, and the
// card's no-personal-range sentence ("a personal range needs more completed
// cycles") and the explainer ("fertile details wait for N completed cycles")
// are one fact told twice. The card carries the information that is specific to
// the moment, so it stays; an account whose cycle is not late keeps the
// explainer, which is then the only place the floor is named. The dashboard,
// the calendar and the stats page all render that card from the same context,
// so the drop lives in BuildOwnerPredictionExplanation, not in one page.
func withoutExplanationRestatedByLateCycle(explanation PredictionExplanation, lateCycle LateCycleNotice) PredictionExplanation {
	if explanation.PrimaryKey == predictionExplainerAwaitingMoreCyclesKey && lateCycle.Visible && lateCycle.MessageKey == LateCycleNoPersonalRangeKey {
		explanation.PrimaryKey = ""
		explanation.PrimaryCount = 0
	}
	return explanation
}

func BuildOwnerPredictionExplanation(user *models.User, cycleContext DashboardCycleContext, hasFactorHint bool) PredictionExplanation {
	if !IsOwnerUser(user) {
		return PredictionExplanation{}
	}

	primaryKey := predictionExplanationPrimaryKey(user, cycleContext)
	explanation := PredictionExplanation{
		PrimaryKey:   primaryKey,
		PrimaryCount: predictionExplanationPrimaryCount(primaryKey),
		SecondaryKey: predictionExplanationSecondaryKey(cycleContext, hasFactorHint),
	}
	if primaryKey == predictionExplainerPregnancyPausedKey {
		explanation.PrimaryDate = cycleContext.PregnancyTestDate
	}
	return withoutExplanationRestatedByLateCycle(explanation, cycleContext.LateCycle)
}

func predictionExplanationPrimaryKey(user *models.User, cycleContext DashboardCycleContext) string {
	switch {
	case cycleContext.PregnancyPaused:
		return predictionExplainerPregnancyPausedKey
	case cycleContext.PredictionDisabled:
		return "prediction.explainer.unpredictable"
	case user != nil && user.IrregularCycle && (cycleContext.DisplayNextPeriodNeedsData || cycleContext.DisplayOvulationNeedsData):
		return predictionExplainerIrregularSparseKey
	case user != nil && user.IrregularCycle && (cycleContext.DisplayNextPeriodUseRange || cycleContext.DisplayOvulationUseRange):
		return "prediction.explainer.irregular_ranges"
	// The first-cycle floor is what makes the cycle ribbon go quiet past the
	// menstrual block, and nothing on the page said so. It sits BELOW the
	// irregular branches deliberately: an irregular owner in the sparse tier has
	// no completed cycle either, and that sentence names the number of cycles
	// the rest of that screen is already counting to. Displacing it would have
	// traded a more specific answer for a more general one. What is left here is
	// the owner who had no explanation at all.
	case cycleContext.AwaitingFirstCycle:
		return predictionExplainerAwaitingFirstCycleKey
	// The next tier up for a regular owner: one or two completed cycles. The
	// fertility half is still withheld, and the sentence names the number the
	// floor counts to. Irregular owners never carry it (their sparse branch
	// above answers first, and the context flag excludes them).
	case cycleContext.AwaitingMoreCycles:
		return predictionExplainerAwaitingMoreCyclesKey
	// Bleeding outside the running cycle's opening episode withholds the
	// fertility half for the day it is logged and the next, and the hero said
	// nothing about it. The branches above withhold that half anyway and name
	// their own reason, so this sentence is for the account they let through.
	case cycleContext.BleedingOutsideCycleStart:
		return predictionExplainerBleedingOutsideKey
	// A regular owner with at least one completed cycle behind them gets no
	// explainer even when the prediction renders as a range: the range is the
	// affordance, and since wave 2 the next-period line names the quantity it
	// shows ("start window"). A sentence saying the range is a range restated
	// the surface instead of adding to it. The first cycle is the exception the
	// branch above now takes, and it is not about how the range is drawn — it is
	// that the fertility half behind it is withheld entirely.
	default:
		return ""
	}
}

func predictionExplanationSecondaryKey(cycleContext DashboardCycleContext, hasFactorHint bool) string {
	if cycleContext.PredictionDisabled || !hasFactorHint {
		return ""
	}
	return "prediction.explainer.factor_context"
}
