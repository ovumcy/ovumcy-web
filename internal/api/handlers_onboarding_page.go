package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) ShowOnboarding(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	if !services.RequiresOnboarding(user) {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/dashboard")
	}

	location := handler.requestLocation(c)
	now := services.DateAtLocation(time.Now().In(location), location)
	data := handler.buildOnboardingViewData(c, user, now, location)
	return handler.render(c, "onboarding", data)
}
