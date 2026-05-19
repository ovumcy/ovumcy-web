package services

import "github.com/ovumcy/ovumcy-web/internal/models"

type PredictionExplanation struct {
	PrimaryKey   string
	SecondaryKey string
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
	case cycleContext.PredictionDisabled:
		return "prediction.explainer.unpredictable"
	case user != nil && user.IrregularCycle && (cycleContext.DisplayNextPeriodNeedsData || cycleContext.DisplayOvulationNeedsData):
		return "prediction.explainer.irregular_sparse"
	case user != nil && user.IrregularCycle && (cycleContext.DisplayNextPeriodUseRange || cycleContext.DisplayOvulationUseRange):
		return "prediction.explainer.irregular_ranges"
	case user != nil && !user.IrregularCycle && cycleContext.DisplayNextPeriodUseRange:
		return "prediction.explainer.variable_ranges"
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
