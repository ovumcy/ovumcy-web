package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) OnboardingStep1(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if !services.RequiresOnboarding(user) {
		return redirectOrJSON(c, "/dashboard")
	}

	today := dateAtLocation(time.Now().In(handler.location), handler.location)
	values, validationError := handler.parseOnboardingStep1Values(c, today)
	if validationError != "" {
		return apiError(c, fiber.StatusBadRequest, validationError)
	}

	handler.ensureDependencies()
	if err := handler.onboardingSvc.SaveStep1(user.ID, values.Start); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to save onboarding step")
	}
	user.LastPeriodStart = &values.Start

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
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if !services.RequiresOnboarding(user) {
		return redirectOrJSON(c, "/dashboard")
	}

	values, validationError := handler.parseOnboardingStep2Input(c)
	if validationError != "" {
		return apiError(c, fiber.StatusBadRequest, validationError)
	}

	handler.ensureDependencies()
	cycleLength, periodLength, err := handler.onboardingSvc.SaveStep2(user.ID, values.CycleLength, values.PeriodLength, values.AutoPeriodFill)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to save onboarding step")
	}
	user.CycleLength = cycleLength
	user.PeriodLength = periodLength
	user.AutoPeriodFill = values.AutoPeriodFill

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.Redirect("/onboarding", fiber.StatusSeeOther)
}
