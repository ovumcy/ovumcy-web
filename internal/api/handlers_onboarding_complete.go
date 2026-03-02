package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) OnboardingComplete(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	if err := services.ValidateOnboardingCompletionEligibility(user); err != nil {
		switch services.ClassifyOnboardingCompletionError(err) {
		case services.OnboardingCompletionErrorNotNeeded:
			return redirectOrJSON(c, "/dashboard")
		case services.OnboardingCompletionErrorStepsRequired:
			return apiError(c, fiber.StatusBadRequest, "complete onboarding steps first")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to finish onboarding")
		}
	}
	_, err := handler.onboardingSvc.CompleteOnboardingForUser(user.ID, handler.location)
	if err != nil {
		if services.ClassifyOnboardingCompletionError(err) == services.OnboardingCompletionErrorStepsRequired {
			return apiError(c, fiber.StatusBadRequest, "complete onboarding steps first")
		}
		return apiError(c, fiber.StatusInternalServerError, "failed to finish onboarding")
	}

	return redirectOrJSON(c, "/dashboard")
}
