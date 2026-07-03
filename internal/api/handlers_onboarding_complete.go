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
		// codecov:ignore:start -- defensive: eligibility (incl. steps-required) is validated above
		// against the request user before CompleteOnboardingForUser re-reads the row, so this
		// post-completion arm only fires on a stale-context / concurrent-clear race; the completion
		// path itself is covered by the e2e onboarding flow.
		if errors.Is(err, services.ErrOnboardingStepsRequired) {
			return handler.respondMappedError(c, onboardingStepsRequiredErrorSpec())
		}
		// codecov:ignore:end
		return handler.respondMappedError(c, onboardingFinishErrorSpec())
	}

	return redirectOrJSON(c, "/dashboard")
}
