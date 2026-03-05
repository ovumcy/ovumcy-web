package api

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestOnboardingErrorSpecs(t *testing.T) {
	testCases := []struct {
		name string
		got  APIErrorSpec
		want APIErrorSpec
	}{
		{
			name: "validation",
			got:  onboardingValidationErrorSpec("invalid input"),
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input"),
		},
		{
			name: "save step",
			got:  onboardingSaveStepErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to save onboarding step"),
		},
		{
			name: "steps required",
			got:  onboardingStepsRequiredErrorSpec(),
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "complete onboarding steps first"),
		},
		{
			name: "finish",
			got:  onboardingFinishErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to finish onboarding"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", testCase.got, testCase.want)
			}
		})
	}
}
