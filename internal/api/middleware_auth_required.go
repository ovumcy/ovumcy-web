package api

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// AuthRequired decides "is this an API request" on the routing-normalized
// path, not the raw one: the router is case-insensitive and ignores a trailing
// slash, so /API/v1/... reaches the same API handler, and must be refused as
// one (a JSON 4xx) rather than redirected to the sign-in page. A redirect is
// below 400, which the per-IP logout row, skipping only failed requests, would
// count.
func (handler *Handler) AuthRequired(c fiber.Ctx) error {
	user, err := handler.authenticateRequest(c)
	if err != nil {
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec := authWebSignInUnavailableErrorSpec()
			if strings.HasPrefix(httpx.RoutingNormalizedPath(c.Path()), "/api/") || acceptsJSON(c) {
				return respondGlobalMappedError(c, spec)
			}
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		}
		if strings.HasPrefix(httpx.RoutingNormalizedPath(c.Path()), "/api/") || acceptsJSON(c) {
			return respondGlobalMappedError(c, unauthorizedErrorSpec())
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	c.Locals(contextUserKey, user)
	if services.RequiresOnboarding(user) && services.ShouldEnforceOnboardingAccess(c.Path()) {
		if strings.HasPrefix(httpx.RoutingNormalizedPath(c.Path()), "/api/") || acceptsJSON(c) {
			return respondGlobalMappedError(c, onboardingRequiredErrorSpec())
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/onboarding")
	}

	return c.Next()
}
