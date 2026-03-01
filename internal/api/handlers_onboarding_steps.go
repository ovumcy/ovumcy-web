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

	if err := handler.saveOnboardingStep1(user, values); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to save onboarding step")
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
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if !services.RequiresOnboarding(user) {
		return redirectOrJSON(c, "/dashboard")
	}

	values, validationError := parseOnboardingStep2Input(c)
	if validationError != "" {
		return apiError(c, fiber.StatusBadRequest, validationError)
	}

	if err := handler.saveOnboardingStep2(user, values); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to save onboarding step")
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.Redirect("/onboarding", fiber.StatusSeeOther)
}
