package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) OnboardingComplete(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	if err := services.ValidateOnboardingCompletionEligibility(user); err != nil {
		switch {
		case errors.Is(err, services.ErrOnboardingCompletionNotNeeded):
			return redirectOrJSON(c, "/dashboard")
		case errors.Is(err, services.ErrOnboardingStepsRequired):
			return handler.respondMappedError(c, onboardingStepsRequiredErrorSpec())
		default:
			return handler.respondMappedError(c, onboardingFinishErrorSpec())
		}
	}
	_, err := handler.onboardingSvc.CompleteOnboardingForUser(c.UserContext(), user.ID, handler.requestLocationFromOnboardingForm(c)) // codecov:ignore -- onboarding completion covered by the e2e onboarding flow
	if err != nil {
		if errors.Is(err, services.ErrOnboardingStepsRequired) {
			return handler.respondMappedError(c, onboardingStepsRequiredErrorSpec())
		}
		return handler.respondMappedError(c, onboardingFinishErrorSpec())
	}

	return redirectOrJSON(c, "/dashboard")
}
