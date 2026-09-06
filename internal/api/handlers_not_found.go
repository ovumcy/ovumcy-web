package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

// notFoundRenderedPath is the address the shared layout renders back into the
// 404 page — the language switcher's `next` field and the outgoing
// `/privacy?back=…` link — in place of the request's own path. On a 404 that
// path is entirely caller-chosen, and the browser branch below renders the full
// layout, so `/victim@example.com/nowhere` would put the address into the markup
// and carry it into a further outgoing URL: browser history, `Referer`, and the
// `/privacy` access log. Every other page's path is its own route, which is why
// this substitution is owed here alone.
//
// It is deliberately not the primary action's target. The primary navigation
// renders on a signed-in 404, and every active state in it is derived from this
// same address, so `/dashboard` would light the "Today" tab and announce
// `aria-current="page"` on a page that is not it. A path matching no navigation
// route highlights nothing, keeps the footer privacy link visible, and — since
// nothing serves it — sends the language switcher back to a 404 in the newly
// chosen language, which is where the visitor actually is.
const notFoundRenderedPath = "/404"

func (handler *Handler) NotFound(c fiber.Ctx) error {
	if strings.HasPrefix(c.Path(), "/api/") || acceptsJSON(c) || isHTMX(c) {
		return respondNotFoundMappedError(c)
	}

	currentUser := handler.optionalAuthenticatedUser(c)
	if currentUser != nil {
		c.Locals(contextUserKey, currentUser)
	}

	primaryPath := "/login"
	primaryLabelKey := "not_found.action_login"
	if currentUser != nil {
		primaryPath = "/dashboard"
		primaryLabelKey = "not_found.action_dashboard"
	}

	c.Status(fiber.StatusNotFound)
	return handler.render(c, "not_found", fiber.Map{
		"Title":           localizedPageTitle(currentMessages(c), "meta.title.not_found", "Ovumcy | Page Not Found"),
		"CurrentPath":     notFoundRenderedPath,
		"CurrentUser":     currentUser,
		"PrimaryPath":     primaryPath,
		"PrimaryLabelKey": primaryLabelKey,
	})
}

// refuseHEADOnShownOnceSurface answers HEAD with the same 404 an unknown path
// receives and lets every other method through. It is the first link in the
// chain of every route named in shownOnceGETRoutes.
//
// Every GET route is served on HEAD too (registerHEADTwins), and a twin runs
// the GET route's own chain. On a shown-once surface that chain performs the
// compare-and-set that makes the reveal single use BEFORE it renders, so a twin
// would spend the owner's one display of a secret to build a response the
// protocol then strips the body from — the mark consumed by a request that
// could not carry what it unlocked. Refusing is also the answer these routes
// already gave for as long as HEAD reached nothing but the catch-all, so no
// client loses a response it used to get.
//
// The refusal rides on the chain rather than on a list held by the composition
// root: the twin copies whatever the GET route carries, so the route table
// states the exception once and an app assembled anywhere — the deployed one,
// this package's test helper — behaves the same way.
func (handler *Handler) refuseHEADOnShownOnceSurface(c fiber.Ctx) error {
	if c.Method() == fiber.MethodHead {
		return handler.NotFound(c)
	}
	return c.Next()
}
