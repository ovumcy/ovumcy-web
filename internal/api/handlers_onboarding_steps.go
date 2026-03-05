package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) OnboardingStep1(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}
	if !services.RequiresOnboarding(user) {
		return redirectOrJSON(c, "/dashboard")
	}

	today := services.DateAtLocation(time.Now().In(handler.location), handler.location)
	values, validationError := handler.parseOnboardingStep1Values(c, today)
	if validationError != "" {
		return handler.respondMappedError(c, onboardingValidationErrorSpec(validationError))
	}
	if err := handler.onboardingSvc.SaveStep1(user.ID, values.Start); err != nil {
		return handler.respondMappedError(c, onboardingSaveStepErrorSpec())
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.Redirect("/onboarding", fiber.StatusSeeOther)
}

func (handler *Handler) OnboardingStep2(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}
	if !services.RequiresOnboarding(user) {
		return redirectOrJSON(c, "/dashboard")
	}

	values, validationError := handler.parseOnboardingStep2Input(c)
	if validationError != "" {
		return handler.respondMappedError(c, onboardingValidationErrorSpec(validationError))
	}
	_, _, err := handler.onboardingSvc.SaveStep2(user.ID, values.CycleLength, values.PeriodLength, values.AutoPeriodFill)
	if err != nil {
		return handler.respondMappedError(c, onboardingSaveStepErrorSpec())
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.Redirect("/onboarding", fiber.StatusSeeOther)
}
