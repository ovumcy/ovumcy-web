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
		if errors.Is(err, services.ErrAuthSessionLookupFailed) {
			return handler.respondSessionFault(c)
		}
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec := authWebSignInUnavailableErrorSpec()
			// The session cookie is already cleared, so a no-JS form's refusal page
			// would link back to a form that only bounces off this gate with no
			// notice: that browser goes straight to the sign-in page.
			if _, plainForm := plainPageFormBackPath(c); plainForm || !(strings.HasPrefix(httpx.RoutingNormalizedPath(c.Path()), "/api/") || acceptsJSON(c)) {
				return handler.redirectSignedOutRefusal(c, spec)
			}
			return handler.respondGlobalMappedError(c, spec)
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
