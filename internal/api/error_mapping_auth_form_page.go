package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
)

// plainAuthFormPagePaths enumerates the plain (no HTMX, no JavaScript
// interception assumed) browser-form auth routes whose CSRF refusal (an idle
// or rotated token) or transport-level rejection (raised before any handler
// runs, so before a domain spec exists to route through respondAuthError) must
// answer as the shared page-form status fragment rather than the JSON
// envelope — the same shape #862 built for POST /lang (WEB-84). A plain <form>
// action is the route key, not the page it renders on.
//
// oidcLinkConfirmPath is the one OIDC-related plain form: /auth/oidc/start,
// /auth/oidc/callback and the logout bridge are none of them a page a browser
// submits (start and the bridge are GET-only; the callback is CSRF-exempt,
// protected instead by the sealed one-time state cookie), so they carry no
// entry here.
//
// Deliberately absent: POST /logout and DELETE /api/v1/sessions/current. Both
// answer the mapped envelope today for a plain browser Accept, and WEB-84
// leaves that unchanged — logout's own answer (it is not a page a refusal can
// re-render: DELETE has no <form>, and POST /logout replaces the page the
// owner was already looking at) is a decision for its own issue.
var plainAuthFormPagePaths = map[string]struct{}{
	"/api/v1/users":                  {},
	"/api/v1/sessions":               {},
	"/api/v1/sessions/2fa-challenge": {},
	"/api/v1/password-resets":        {},
	"/api/v1/password-resets/redeem": {},
	oidcLinkConfirmPath:              {},
}

// isPlainAuthFormPageNavigation reports whether c is a plain HTML POST to one
// of the routes above: the app's public sign-in surface, submitted by neither
// HTMX nor a JSON client. Scoped to POST because every one of these routes is
// POST-only; a CSRF refusal on any other method there is unrouted and answers
// like one everywhere else.
func isPlainAuthFormPageNavigation(c fiber.Ctx) bool {
	if c.Method() != fiber.MethodPost {
		return false
	}
	if responseFormat(c) != httpx.ResponseFormatHTML {
		return false
	}
	_, ok := plainAuthFormPagePaths[httpx.RoutingNormalizedPath(c.Path())]
	return ok
}
