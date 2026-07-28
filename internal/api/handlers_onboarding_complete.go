package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Completing onboarding writes day entries: with auto-period-fill on it seeds
// one is_period daily_logs row per day of the declared first period. That makes
// it a health-data mutation in its own right, distinct from the cycle-settings
// columns the two steps write, so it carries the day_entry target the day-write
// path carries rather than cycle_settings.
var onboardingCompleteMutation = healthMutationKind{action: "onboarding.complete", target: "day_entry"}

func (handler *Handler) OnboardingComplete(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, onboardingCompleteMutation, unauthorizedErrorSpec())
	}

	if err := services.ValidateOnboardingCompletionEligibility(user); err != nil {
		switch {
		case errors.Is(err, services.ErrOnboardingCompletionNotNeeded):
			return redirectOrJSON(c, "/dashboard")
		case errors.Is(err, services.ErrOnboardingStepsRequired):
			return handler.failMutation(c, onboardingCompleteMutation, onboardingStepsRequiredErrorSpec())
		default:
			return handler.failMutation(c, onboardingCompleteMutation, onboardingFinishErrorSpec()) // codecov:ignore -- unreachable: ValidateOnboardingCompletionEligibility returns only the two sentinels above or nil.
		}
	}
	_, err := handler.onboardingSvc.CompleteOnboardingForUser(c.Context(), user.ID, handler.requestLocationFromOnboardingForm(c)) // codecov:ignore -- onboarding completion covered by the e2e onboarding flow
	if err != nil {
		// codecov:ignore:start -- defensive: eligibility (incl. steps-required) is validated above
		// against the request user before CompleteOnboardingForUser re-reads the row, so this
		// post-completion arm only fires on a stale-context / concurrent-clear race; the completion
		// path itself is covered by the e2e onboarding flow.
		if errors.Is(err, services.ErrOnboardingStepsRequired) {
			return handler.failMutation(c, onboardingCompleteMutation, onboardingStepsRequiredErrorSpec())
		}
		// codecov:ignore:end
		return handler.failMutation(c, onboardingCompleteMutation, onboardingFinishErrorSpec())
	}

	handler.logMutationSuccess(c, onboardingCompleteMutation)
	return redirectOrJSON(c, "/dashboard")
}
