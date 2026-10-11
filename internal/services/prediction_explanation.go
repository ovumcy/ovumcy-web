package services

import "github.com/ovumcy/ovumcy-web/internal/models"

type PredictionExplanation struct {
	PrimaryKey   string
	SecondaryKey string
}

// The explainer keys whose sentence names the completed-cycle floor. The two
// are also the whole set PredictionExplanationNamesCycleFloor answers for.
const (
	predictionExplainerIrregularSparseKey    = "prediction.explainer.irregular_sparse"
	predictionExplainerAwaitingMoreCyclesKey = "prediction.explainer.awaiting_more_cycles"
)

// PredictionExplanationNamesCycleFloor reports whether the explainer sentence
// under key carries the completed-cycle floor as its one %d. The templates ask
// it before formatting, so a key that takes no argument is never handed one.
func PredictionExplanationNamesCycleFloor(key string) bool {
	return key == predictionExplainerIrregularSparseKey || key == predictionExplainerAwaitingMoreCyclesKey
}

// withoutExplanationRestatedByLateCycle drops the awaiting-more-cycles explainer
// when the late-cycle card above it already says the same thing. A cycle that
// runs late on one or two completed cycles has no personal range yet, and the
// card's no-personal-range sentence ("a personal range needs more completed
// cycles") and the explainer ("fertile details wait for N completed cycles")
// are one fact told twice. The card carries the information that is specific to
// the moment, so it stays; an account whose cycle is not late keeps the
// explainer, which is then the only place the floor is named.
func withoutExplanationRestatedByLateCycle(explanation PredictionExplanation, lateCycle LateCycleNotice) PredictionExplanation {
	if explanation.PrimaryKey == predictionExplainerAwaitingMoreCyclesKey && lateCycle.Visible && lateCycle.MessageKey == LateCycleNoPersonalRangeKey {
		explanation.PrimaryKey = ""
	}
	return explanation
}

func BuildOwnerPredictionExplanation(user *models.User, cycleContext DashboardCycleContext, hasFactorHint bool) PredictionExplanation {
	if !IsOwnerUser(user) {
		return PredictionExplanation{}
	}

	explanation := PredictionExplanation{
		PrimaryKey:   predictionExplanationPrimaryKey(user, cycleContext),
		SecondaryKey: predictionExplanationSecondaryKey(cycleContext, hasFactorHint),
	}
	return explanation
}

func predictionExplanationPrimaryKey(user *models.User, cycleContext DashboardCycleContext) string {
	switch {
	case cycleContext.PregnancyPaused:
		return "prediction.explainer.pregnancy_paused"
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
		return "prediction.explainer.awaiting_first_cycle"
	// The next tier up for a regular owner: one or two completed cycles. The
	// fertility half is still withheld, and the sentence names the number the
	// floor counts to. Irregular owners never carry it (their sparse branch
	// above answers first, and the context flag excludes them).
	case cycleContext.AwaitingMoreCycles:
		return predictionExplainerAwaitingMoreCyclesKey
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
