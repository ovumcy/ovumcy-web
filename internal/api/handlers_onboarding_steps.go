package api

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Onboarding writes a subset of the users columns the settings cycle form
// writes — step 1 sets last_period_start, step 2 sets cycle_length/
// period_length/luteal_phase/auto_period_fill/irregular_cycle/usage_goal (age
// is asked for in settings only) — so both carry the target the settings
// surface carries. An operator asking "were the
// cycle settings changed?" filters on domain+target, and that filter must not
// depend on which of the two surfaces produced the change.
var (
	onboardingCycleStartMutation = healthMutationKind{action: "onboarding.cycle_start_update", target: "cycle_settings"}
	onboardingCycleMutation      = healthMutationKind{action: "onboarding.cycle_update", target: "cycle_settings"}
)

func (handler *Handler) OnboardingStep1(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, onboardingCycleStartMutation, unauthorizedErrorSpec())
	}
	if !services.RequiresOnboarding(user) {
		return redirectOrJSON(c, "/dashboard")
	}

	location := handler.requestLocationFromOnboardingForm(c)
	today := services.DateAtLocation(time.Now().In(location), location)
	values, validationError := handler.parseOnboardingStep1Values(c, today, location)
	if validationError != "" {
		return handler.failMutation(c, onboardingCycleStartMutation, onboardingValidationErrorSpec(validationError))
	}
	if err := handler.onboardingSvc.SaveStep1(c.Context(), user.ID, values.Start); err != nil {
		return handler.failMutation(c, onboardingCycleStartMutation, onboardingSaveStepErrorSpec())
	}

	handler.logMutationSuccess(c, onboardingCycleStartMutation)

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To("/onboarding?step=2")
}

func (handler *Handler) OnboardingStep2(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, onboardingCycleMutation, unauthorizedErrorSpec())
	}
	if !services.RequiresOnboarding(user) {
		return redirectOrJSON(c, "/dashboard")
	}

	_ = handler.requestLocationFromOnboardingForm(c)

	values, validationError := handler.parseOnboardingStep2Input(c)
	if validationError != "" {
		return handler.failMutation(c, onboardingCycleMutation, onboardingValidationErrorSpec(validationError))
	}
	_, _, err := handler.onboardingSvc.SaveStep2(
		c.Context(),
		user.ID,
		values.CycleLength,
		values.PeriodLength,
		values.AutoPeriodFill,
		values.IrregularCycle,
		values.UsageGoal,
	)
	if err != nil {
		return handler.failMutation(c, onboardingCycleMutation, onboardingSaveStepErrorSpec())
	}
	if _, err := handler.onboardingSvc.CompleteOnboardingForUser(c.Context(), user.ID, handler.requestLocationFromOnboardingForm(c)); err != nil {
		if errors.Is(err, services.ErrOnboardingStepsRequired) {
			// The step-2 columns are persisted at this point; only the completion
			// (which seeds the first period's day entries) did not run, so the
			// mutation is audited as a success and the caller is sent back to step 1.
			handler.logMutationSuccess(c, onboardingCycleMutation)
			if acceptsJSON(c) {
				return c.JSON(fiber.Map{"ok": true})
			}
			if isHTMX(c) {
				return c.SendStatus(fiber.StatusNoContent)
			}
			return c.Redirect().Status(fiber.StatusSeeOther).To("/onboarding?step=1")
		}
		return handler.failMutation(c, onboardingCycleMutation, onboardingFinishErrorSpec())
	}
	handler.logMutationSuccess(c, onboardingCycleMutation)
	if isHTMX(c) {
		c.Set("HX-Redirect", "/dashboard")
		return c.SendStatus(fiber.StatusNoContent)
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To("/dashboard")
}
