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

// onboardingAccountLanguage is the language the onboarding pages rendered in,
// handed to completion as the account's language when it has none stored. Only
// a code this build ships is passed on, normalized; anything else is "" so no
// language nobody picked is pinned to the account.
func (handler *Handler) onboardingAccountLanguage(c fiber.Ctx) string {
	language := currentLanguage(c)
	if !handler.i18n.IsSupportedLanguage(language) {
		return ""
	}
	return handler.i18n.NormalizeLanguage(language)
}

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
	_, err := handler.onboardingSvc.CompleteOnboardingForUser(c.Context(), user.ID, handler.clockNow(), handler.requestLocationFromOnboardingForm(c), handler.onboardingAccountLanguage(c)) // codecov:ignore -- onboarding completion covered by the e2e onboarding flow
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
