package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) OnboardingComplete(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	if err := services.ValidateOnboardingCompletionEligibility(user); err != nil {
		switch services.ClassifyOnboardingCompletionError(err) {
		case services.OnboardingCompletionErrorNotNeeded:
			return redirectOrJSON(c, "/dashboard")
		case services.OnboardingCompletionErrorStepsRequired:
			return handler.respondMappedError(c, onboardingStepsRequiredErrorSpec())
		default:
			return handler.respondMappedError(c, onboardingFinishErrorSpec())
		}
	}
	_, err := handler.onboardingSvc.CompleteOnboardingForUser(user.ID, handler.location)
	if err != nil {
		if services.ClassifyOnboardingCompletionError(err) == services.OnboardingCompletionErrorStepsRequired {
			return handler.respondMappedError(c, onboardingStepsRequiredErrorSpec())
		}
		return handler.respondMappedError(c, onboardingFinishErrorSpec())
	}

	return redirectOrJSON(c, "/dashboard")
}
