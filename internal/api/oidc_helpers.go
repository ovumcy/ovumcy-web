package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

func (handler *Handler) oidcEnabled() bool {
	return handler != nil && handler.oidcService != nil && handler.oidcService.Enabled()
}

func (handler *Handler) localPublicAuthEnabled() bool {
	if handler == nil || handler.oidcService == nil {
		return true
	}
	return handler.oidcService.LocalPublicAuthEnabled()
}

// oidcResponseModeQuery reports whether the provider returns the authorization
// code via a query redirect (GET) rather than a form_post body. It gates the
// GET callback route registration and the callback parameter source.
func (handler *Handler) oidcResponseModeQuery() bool {
	if handler == nil || handler.oidcService == nil {
		return false
	}
	return handler.oidcService.ResponseMode() == security.OIDCResponseModeQuery
}

// oidcCallbackValue reads a callback parameter from EXACTLY ONE source keyed by
// the response mode: the URL query in query mode, the POST body otherwise.
// Reading a single source (never both) is deliberate — mixing them would open a
// parameter-pollution seam where an attacker-supplied query value could ride
// alongside the legitimate body value (or vice versa). fiber.Ctx.FormValue
// (fasthttp) falls back to the query string, so it is NOT body-strict; the
// body path therefore reads PostArgs directly and the query path reads Query
// directly. The sealed one-time state cookie is the actual authorization check
// either way.
func (handler *Handler) oidcCallbackValue(c fiber.Ctx, name string) string {
	if handler.oidcResponseModeQuery() {
		return c.Query(name)
	}
	return string(c.Request().PostArgs().Peek(name))
}
