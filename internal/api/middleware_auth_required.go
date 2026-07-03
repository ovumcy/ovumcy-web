package api

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) AuthRequired(c fiber.Ctx) error {
	user, err := handler.authenticateRequest(c)
	if err != nil {
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec := authWebSignInUnavailableErrorSpec()
			if strings.HasPrefix(c.Path(), "/api/") || acceptsJSON(c) {
				return respondGlobalMappedError(c, spec)
			}
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		}
		if strings.HasPrefix(c.Path(), "/api/") || acceptsJSON(c) {
			return respondGlobalMappedError(c, unauthorizedErrorSpec())
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	c.Locals(contextUserKey, user)
	if services.RequiresOnboarding(user) && services.ShouldEnforceOnboardingAccess(c.Path()) {
		if strings.HasPrefix(c.Path(), "/api/") || acceptsJSON(c) {
			return respondGlobalMappedError(c, onboardingRequiredErrorSpec())
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/onboarding")
	}

	return c.Next()
}
