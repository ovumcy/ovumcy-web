package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) OnboardingStep1(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}
	if !services.RequiresOnboarding(user) {
		return redirectOrJSON(c, "/dashboard")
	}

	location := handler.requestLocationFromOnboardingForm(c)
	today := services.DateAtLocation(time.Now().In(location), location)
	values, validationError := handler.parseOnboardingStep1Values(c, today, location)
	if validationError != "" {
		return handler.respondMappedError(c, onboardingValidationErrorSpec(validationError))
	}
	if err := handler.onboardingSvc.SaveStep1(c.UserContext(), user.ID, values.Start); err != nil {
		return handler.respondMappedError(c, onboardingSaveStepErrorSpec())
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.Redirect("/onboarding?step=2", fiber.StatusSeeOther)
}

func (handler *Handler) OnboardingStep2(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}
	if !services.RequiresOnboarding(user) {
		return redirectOrJSON(c, "/dashboard")
	}

	_ = handler.requestLocationFromOnboardingForm(c)

	values, validationError := handler.parseOnboardingStep2Input(c)
	if validationError != "" {
		return handler.respondMappedError(c, onboardingValidationErrorSpec(validationError))
	}
	_, _, err := handler.onboardingSvc.SaveStep2(
		c.UserContext(),
		user.ID,
		values.CycleLength,
		values.PeriodLength,
		values.AutoPeriodFill,
		values.IrregularCycle,
		values.AgeGroup,
		values.UsageGoal,
	)
	if err != nil {
		return handler.respondMappedError(c, onboardingSaveStepErrorSpec())
	}
	if _, err := handler.onboardingSvc.CompleteOnboardingForUser(c.UserContext(), user.ID, handler.requestLocationFromOnboardingForm(c)); err != nil {
		if services.ClassifyOnboardingCompletionError(err) == services.OnboardingCompletionErrorStepsRequired {
			if acceptsJSON(c) {
				return c.JSON(fiber.Map{"ok": true})
			}
			if isHTMX(c) {
				return c.SendStatus(fiber.StatusNoContent)
			}
			return c.Redirect("/onboarding?step=1", fiber.StatusSeeOther)
		}
		return handler.respondMappedError(c, onboardingFinishErrorSpec())
	}
	if isHTMX(c) {
		c.Set("HX-Redirect", "/dashboard")
		return c.SendStatus(fiber.StatusNoContent)
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/dashboard", fiber.StatusSeeOther)
}
