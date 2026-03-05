package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) AuthRequired(c *fiber.Ctx) error {
	user, err := handler.authenticateRequest(c)
	if err != nil {
		if strings.HasPrefix(c.Path(), "/api/") {
			return respondGlobalMappedError(c, unauthorizedErrorSpec())
		}
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	c.Locals(contextUserKey, user)
	if services.RequiresOnboarding(user) && services.ShouldEnforceOnboardingAccess(c.Path()) {
		if strings.HasPrefix(c.Path(), "/api/") {
			return respondGlobalMappedError(c, onboardingRequiredErrorSpec())
		}
		return c.Redirect("/onboarding", fiber.StatusSeeOther)
	}

	return c.Next()
}
