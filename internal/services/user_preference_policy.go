package services

import (
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func NormalizeAgeGroup(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case models.AgeGroupUnder20:
		return models.AgeGroupUnder20
	case models.AgeGroup20To35:
		return models.AgeGroup20To35
	case models.AgeGroup35Plus:
		return models.AgeGroup35Plus
	default:
		return models.AgeGroupUnknown
	}
}

func NormalizeUsageGoal(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case models.UsageGoalAvoid:
		return models.UsageGoalAvoid
	case models.UsageGoalTrying:
		return models.UsageGoalTrying
	default:
		return models.UsageGoalHealth
	}
}

func UsageGoalTranslationKey(value string) string {
	switch NormalizeUsageGoal(value) {
	case models.UsageGoalAvoid:
		return "settings.goal.avoid"
	case models.UsageGoalTrying:
		return "settings.goal.trying"
	default:
		return "settings.goal.health"
	}
}

func UsageGoalSummaryTranslationKey(value string) string {
	switch NormalizeUsageGoal(value) {
	case models.UsageGoalAvoid:
		return "usage_goal.summary.avoid"
	case models.UsageGoalTrying:
		return "usage_goal.summary.trying"
	default:
		return "usage_goal.summary.health"
	}
}

func AgeGroupTranslationKey(value string) string {
	switch NormalizeAgeGroup(value) {
	case models.AgeGroupUnder20:
		return "settings.age_group.under_20"
	case models.AgeGroup35Plus:
		return "settings.age_group.35_plus"
	default:
		return "settings.age_group.20_35"
	}
}
