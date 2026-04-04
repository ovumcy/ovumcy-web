package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestNormalizeAgeGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "under 20", input: "under_20", want: models.AgeGroupUnder20},
		{name: "trimmed 35 plus", input: "  AGE_35_PLUS  ", want: models.AgeGroup35Plus},
		{name: "regular range", input: "age_20_35", want: models.AgeGroup20To35},
		{name: "unknown fallback", input: "not-a-group", want: models.AgeGroupUnknown},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := NormalizeAgeGroup(testCase.input); got != testCase.want {
				t.Fatalf("NormalizeAgeGroup(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestNormalizeUsageGoal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "avoid pregnancy", input: "avoid_pregnancy", want: models.UsageGoalAvoid},
		{name: "trimmed trying", input: "  TRYING_TO_CONCEIVE  ", want: models.UsageGoalTrying},
		{name: "health fallback", input: "not-a-goal", want: models.UsageGoalHealth},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := NormalizeUsageGoal(testCase.input); got != testCase.want {
				t.Fatalf("NormalizeUsageGoal(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestUserPreferenceTranslationKeysUseNormalizedFallbacks(t *testing.T) {
	t.Parallel()

	if got := UsageGoalTranslationKey("unknown"); got != "settings.goal.health" {
		t.Fatalf("expected health translation fallback, got %q", got)
	}
	if got := UsageGoalTranslationKey(models.UsageGoalAvoid); got != "settings.goal.avoid" {
		t.Fatalf("expected avoid translation key, got %q", got)
	}
	if got := UsageGoalSummaryTranslationKey("unknown"); got != "usage_goal.summary.health" {
		t.Fatalf("expected health summary translation fallback, got %q", got)
	}
	if got := UsageGoalSummaryTranslationKey(models.UsageGoalTrying); got != "usage_goal.summary.trying" {
		t.Fatalf("expected trying summary translation key, got %q", got)
	}
	if got := AgeGroupTranslationKey("unknown"); got != "settings.age_group.20_35" {
		t.Fatalf("expected middle age fallback translation key, got %q", got)
	}
	if got := AgeGroupTranslationKey(models.AgeGroup35Plus); got != "settings.age_group.35_plus" {
		t.Fatalf("expected 35+ translation key, got %q", got)
	}
}
