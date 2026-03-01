package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) OnboardingComplete(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if !services.RequiresOnboarding(user) {
		return redirectOrJSON(c, "/dashboard")
	}
	if user.LastPeriodStart == nil {
		return apiError(c, fiber.StatusBadRequest, "complete onboarding steps first")
	}

	startDay, err := handler.completeOnboardingForUser(user.ID)
	if err != nil {
		if errors.Is(err, errOnboardingStepsRequired) {
			return apiError(c, fiber.StatusBadRequest, "complete onboarding steps first")
		}
		return apiError(c, fiber.StatusInternalServerError, "failed to finish onboarding")
	}

	user.OnboardingCompleted = true
	user.LastPeriodStart = &startDay
	return redirectOrJSON(c, "/dashboard")
}
