package services

import (
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// NormalizeAgeGroup accepts the current bracket values and returns
// AgeGroupUnknown for anything else, including the legacy under_20 /
// age_20_35 / age_35_plus values stored before the medically-recalibrated
// bracketing landed. Legacy answers conflated the lowest-variability cohort
// (35–39) with the perimenopausal cohort (45+), so they are intentionally
// not migrated to a guessed new bucket — affected users see the current
// question with no preselected answer on the next settings or onboarding
// edit.
func NormalizeAgeGroup(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case models.AgeGroupUnder40:
		return models.AgeGroupUnder40
	case models.AgeGroup40To45:
		return models.AgeGroup40To45
	case models.AgeGroup45Plus:
		return models.AgeGroup45Plus
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
	case models.AgeGroupUnder40:
		return "settings.age_group.under_40"
	case models.AgeGroup40To45:
		return "settings.age_group.40_to_45"
	case models.AgeGroup45Plus:
		return "settings.age_group.45_plus"
	default:
		return "settings.age_group.unknown"
	}
}
