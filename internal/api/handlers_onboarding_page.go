package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) ShowOnboarding(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	if !services.RequiresOnboarding(user) {
		return c.Redirect("/dashboard", fiber.StatusSeeOther)
	}

	now := dateAtLocation(time.Now().In(handler.location), handler.location)
	data := handler.buildOnboardingViewData(c, user, now)
	return handler.render(c, "onboarding", data)
}
