package api

import "github.com/gofiber/fiber/v2"

func onboardingValidationErrorSpec(key string) APIErrorSpec {
	return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, key)
}

func onboardingSaveStepErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to save onboarding step")
}

func onboardingStepsRequiredErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "complete onboarding steps first")
}

func onboardingFinishErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to finish onboarding")
}
