package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
)

// calendarFeedRouteShapeMarker is the body a probe app answers with when the
// request actually reached the registered ":token.ics" route, as opposed to
// falling through to the probe's own 404.
const calendarFeedRouteShapeMarker = "MARKER"

// calendarFeedDispatchProbeApp registers ONLY the feed's own route pattern
// (with a marker handler) over the shipped fiberConfig — no catch-all, no
// sibling route competing for the same prefix — so a spelling's outcome
// reflects fiber's router alone. fiberConfig, not a bare fiber.New(): the
// shipped config leaves CaseSensitive and StrictRouting both off, and that is
// exactly the normalization api.IsCalendarFeedRequest has to agree with — a
// wiring test has to build on what the shipped app actually routes, not on a
// bare default that happens to share today's flags.
func calendarFeedDispatchProbeApp(t *testing.T) *fiber.App {
	t.Helper()

	app := fiber.New(fiberConfig(proxySettings{}))
	app.Get("/calendar/feed/:token.ics", func(c fiber.Ctx) error {
		return c.SendString(calendarFeedRouteShapeMarker)
	})
	return app
}

// calendarFeedDispatchReached reports whether method+path actually reached
// the probe app's marker handler. HEAD carries no body on the wire, so a
// reached HEAD is read off the status (200 from the marker handler's
// SendString) rather than the (always empty) body fasthttp strips for it.
func calendarFeedDispatchReached(t *testing.T, app *fiber.App, method, path string) bool {
	t.Helper()

	request := httptest.NewRequest(method, path, nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()

	if method == http.MethodHead {
		return response.StatusCode == http.StatusOK
	}
	return string(mustReadAll(t, response)) == calendarFeedRouteShapeMarker
}

// TestIsCalendarFeedRequestMatchesWhatFiberActuallyDispatches is
// api.IsCalendarFeedRequest's definition test: for every (method, path) pair
// below, the predicate must agree with whether fiber's OWN router — driven
// directly, not modeled — actually reaches ServeCalendarFeed's route pattern.
// This is the experimental ground truth for how fiber binds ":token.ics",
// established by running the real router rather than assumed from fiber's
// docs; the spellings include every one from rate_limit_scope_guard_test.go's
// routableSpellings plus the edge cases a route-parameter pattern raises on
// its own (a dot inside the token, an empty token, a nested path segment, a
// case-folded prefix).
//
// GET/HEAD reaching this bare probe app (no catch-all ahead of the route) is
// not by itself an account of what the REAL production route table dispatches:
// fiber appends a GET route's auto-generated HEAD copy (router.go's
// ensureAutoHeadRoutesLocked) only at serve time, behind every
// directly-registered Use middleware — including the terminal
// app.Use(handler.NotFound) that cmd/ovumcy/server.go's newFiberApp mounts
// after api.RegisterRoutes — so a HEAD request to any page route used to reach
// that catch-all instead of its own handler. api.RegisterRoutes now registers
// the HEAD twins itself, ahead of anything mounted afterwards, and
// head_route_dispatch_test.go pins that on the production stack. What was true
// throughout, and is what this test needs: CSRF and LanguageMiddleware are
// mounted as Use before api.RegisterRoutes even runs, so they act on a HEAD
// request to this path however it is dispatched — which is why the predicate
// has to say yes to HEAD.
func TestIsCalendarFeedRequestMatchesWhatFiberActuallyDispatches(t *testing.T) {
	app := calendarFeedDispatchProbeApp(t)
	canonical := "/calendar/feed/" + strings.Repeat("A", 48) + ".ics"

	spellings := append([]string{canonical}, routableSpellings(canonical)...)
	spellings = append(spellings,
		"/calendar/feed/a.b.ics",
		"/calendar/feed/.ics",
		"/calendar/feed/x.ICS",
		"/calendar/feed/x.ics/",
		"/calendar/feed/x.ics//",
		"/Calendar/Feed/x.ics",
		"/calendar/feed/a/b.ics",
		"/calendar/feed/",
		"/calendar/feed",
		"/calendar/feedback",
		// An empty token immediately followed by a literal "/" (rest starts
		// with "/" itself), distinct from the mid-token "/" in
		// "/calendar/feed/a/b.ics" below — and a neighbour that continues the
		// prefix's characters past a full extra path segment rather than a
		// bare "back" suffix. Neither is the feed: the predicate must refuse
		// both, and no route answers either.
		"/calendar/feed//x.ics",
		"/calendar/feedback/x.ics",
		// fiber's router finds the token/suffix boundary at the FIRST ".ics" in
		// the remainder (path.go's findParamLen), not the last: these four all
		// carry a second ".ics" (or start with one) further right, which the
		// router treats as trailing garbage after a shorter token and refuses —
		// a HasSuffix-and-slice reading of the same string would find only the
		// LAST ".ics" and wrongly call every one of them a match.
		"/calendar/feed/a.ics.ics",
		"/calendar/feed/.ics.ics",
		"/calendar/feed/a.icsb.ics",
		"/calendar/feed/a.ICS.ics",
		// fiberConfig leaves UnescapePath at its default (false): the router
		// matches these RAW, undecoded bytes, never the decoded form, and the
		// predicate has to agree on the same raw bytes rather than silently
		// decoding first.
		"/calendar/feed/%2E%2E.ics",
		"/calendar/feed/a%2Fb.ics",
		"/calendar/feed/x.ics%2F",
	)

	for _, path := range spellings {
		for _, method := range fiber.DefaultMethods {
			t.Run(method+" "+path, func(t *testing.T) {
				reached := calendarFeedDispatchReached(t, app, method, path)
				predicted := api.IsCalendarFeedRequest(method, path)
				if predicted != reached {
					t.Fatalf("IsCalendarFeedRequest(%q, %q) = %t, but fiber's own router reached the route = %t", method, path, predicted, reached)
				}
			})
		}
	}
}
