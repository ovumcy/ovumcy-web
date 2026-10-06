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
		// A fault is not a refusal: the session may be live, so neither the
		// sign-in redirect nor the "not signed in" notice may answer it.
		if errors.Is(err, errAuthSessionUnresolved) {
			return handler.respondGlobalMappedError(c, transportErrorSpecForStatus(fiber.StatusInternalServerError))
		}
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec := authWebSignInUnavailableErrorSpec()
			// The session cookie is already cleared, so the refusal page's link
			// back to the form would only bounce off this gate with no notice.
			if _, ok := plainPageFormBackPath(c); ok {
				return handler.redirectSignedOutRefusal(c, spec)
			}
			if strings.HasPrefix(httpx.RoutingNormalizedPath(c.Path()), "/api/") || acceptsJSON(c) {
				return handler.respondGlobalMappedError(c, spec)
			}
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		}
		if strings.HasPrefix(httpx.RoutingNormalizedPath(c.Path()), "/api/") || acceptsJSON(c) {
			return handler.respondGlobalMappedError(c, unauthorizedErrorSpec())
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	c.Locals(contextUserKey, user)
	if services.RequiresOnboarding(user) {
		path := httpx.RoutingNormalizedPath(c.Path())
		if services.ShouldEnforceOnboardingAccess(path) {
			if strings.HasPrefix(path, "/api/") || acceptsJSON(c) {
				return handler.respondGlobalMappedError(c, onboardingRequiredErrorSpec())
			}
			return c.Redirect().Status(fiber.StatusSeeOther).To("/onboarding")
		}
	}

	return c.Next()
}
