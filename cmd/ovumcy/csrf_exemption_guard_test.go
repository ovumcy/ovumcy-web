package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// These tests are the forward-looking guard for the CSRF exemption list
// (mirroring the style of internal/api/owner_only_coverage_regression_test.go).
// The security contract is: every state-mutating route is CSRF-protected at
// the middleware layer, with a SINGLE exemption — POST /auth/oidc/callback —
// which is instead protected by the sealed one-time state cookie. Adding a
// second exemption to csrfMiddlewareConfig.Next (or unmounting the middleware)
// must fail here until this expected list is consciously updated together
// with a security review.
//
// OIDC_RESPONSE_MODE=query additionally serves the callback over GET (the
// provider returns the code as a query redirect). GET is a safe method that the
// CSRF middleware never validates, so it needs no entry on the exemption list;
// it is protected by the SAME sealed one-time state cookie as the POST callback
// (matchesState + validAt reading the state from the query). The mutating-method
// exemption below therefore stays a single POST route regardless of mode.

// csrfGuardExpectedExemptions is the complete, reviewed exemption list.
var csrfGuardExpectedExemptions = map[string]struct{}{
	http.MethodPost + " " + security.OIDCCallbackPath: {},
}

func csrfMutatingMethods() map[string]struct{} {
	return map[string]struct{}{
		http.MethodPost:   {},
		http.MethodPut:    {},
		http.MethodPatch:  {},
		http.MethodDelete: {},
	}
}

// newCSRFGuardTestApp builds the app exactly as the production composition
// root does (newFiberApp: full middleware chain including the CSRF middleware,
// the real route table, the NotFound catch-all), backed by a temp SQLite DB.
// Rate limits are set high so walking every route cannot trip a limiter.
func newCSRFGuardTestApp(t *testing.T) *fiber.App {
	t.Helper()

	handler := newRateLimitTestHandler(t)
	config := runtimeConfig{
		Location:        time.UTC,
		DefaultLanguage: "en",
		CookieSecure:    false,
		RateLimits: rateLimitSettings{
			LoginMax:             100000,
			LoginWindow:          time.Hour,
			ForgotPasswordMax:    100000,
			ForgotPasswordWindow: time.Hour,
			RegisterMax:          100000,
			RegisterWindow:       time.Hour,
			LogoutMax:            100000,
			LogoutWindow:         time.Hour,
			APIMax:               100000,
			APIWindow:            time.Hour,
		},
	}
	return newFiberApp(config, handler)
}

// concreteCSRFProbePath substitutes route parameters with representative
// concrete values so the probe requests hit real router paths.
func concreteCSRFProbePath(routePath string) string {
	replacements := map[string]string{
		":date": "2026-01-15",
		":id":   "1",
	}
	path := routePath
	for placeholder, value := range replacements {
		path = strings.ReplaceAll(path, placeholder, value)
	}
	return path
}

// mutatingRoutePathsByKey walks the real route table and returns every unique
// state-mutating "METHOD path" pair with its concrete probe path.
func mutatingRoutePathsByKey(t *testing.T, app *fiber.App) map[string]string {
	t.Helper()

	mutating := csrfMutatingMethods()
	routes := map[string]string{}
	for _, route := range app.GetRoutes() {
		if _, ok := mutating[route.Method]; !ok {
			continue
		}
		key := route.Method + " " + route.Path
		routes[key] = concreteCSRFProbePath(route.Path)
	}
	if len(routes) == 0 {
		t.Fatal("expected at least one state-mutating route; recheck route discovery")
	}
	return routes
}

// TestCSRFExemptionListIsExactlyOneRoute evaluates the REAL production
// csrfMiddlewareConfig.Next predicate against every state-mutating route the
// app registers and asserts the set of exempted routes is exactly
// csrfGuardExpectedExemptions (a single entry). It also pins the exemption to
// the POST method: a GET to the same path must not be exempt.
func TestCSRFExemptionListIsExactlyOneRoute(t *testing.T) {
	app := newCSRFGuardTestApp(t)
	next := csrfMiddlewareConfig(false, newRateLimitTestHandler(t)).Next
	if next == nil {
		t.Fatal("csrfMiddlewareConfig.Next is nil: the exemption predicate is gone; update this guard together with the middleware change")
	}

	// Probe app: reports through the status code whether Next would exempt the
	// incoming (method, path) from CSRF validation.
	probe := fiber.New()
	probe.Use(func(c fiber.Ctx) error {
		if next(c) {
			return c.SendStatus(fiber.StatusTeapot)
		}
		return c.SendStatus(fiber.StatusOK)
	})
	isExempt := func(method string, path string) bool {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(""))
		response, err := probe.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("probe %s %s: %v", method, path, err)
		}
		defer func() { _ = response.Body.Close() }()
		return response.StatusCode == fiber.StatusTeapot
	}

	exempted := map[string]struct{}{}
	for key, probePath := range mutatingRoutePathsByKey(t, app) {
		method := strings.SplitN(key, " ", 2)[0]
		if isExempt(method, probePath) {
			exempted[key] = struct{}{}
		}
	}

	if len(exempted) != len(csrfGuardExpectedExemptions) {
		t.Fatalf("expected exactly %d CSRF exemption(s), got %d: %v — a new exemption requires a conscious security review and an update to csrfGuardExpectedExemptions",
			len(csrfGuardExpectedExemptions), len(exempted), exempted)
	}
	for key := range csrfGuardExpectedExemptions {
		if _, ok := exempted[key]; !ok {
			t.Fatalf("expected %q to be the sole CSRF exemption, but the predicate no longer exempts it (got %v)", key, exempted)
		}
	}

	// The exemption predicate is bound to POST: a GET to the same path must not
	// match it. This is not a weakening of the query-mode GET callback — GET is
	// a safe method the CSRF middleware never validates, so it needs no
	// predicate exemption and is protected by the sealed one-time state cookie.
	if isExempt(http.MethodGet, security.OIDCCallbackPath) {
		t.Fatalf("CSRF exemption must be bound to POST: GET %s is unexpectedly exempt", security.OIDCCallbackPath)
	}
}

// TestCSRFPredicateSkipsTheCookielessFeedGETAndHEADWithoutWideningTheMutatingExemptions
// covers the predicate's OTHER clause: the one that stops csrf.New from
// minting and setting its own cookie on the cookieless calendar feed (a safe
// method it was never going to validate anyway — see the Next comment on
// csrfMiddlewareConfig). That clause is not in csrfGuardExpectedExemptions
// because it exempts no mutating route; TestCSRFExemptionListIsExactlyOneRoute
// above proves that count stayed at exactly one by construction, since it
// walks only state-mutating routes and the feed registers none. This test
// pins the other half directly: the predicate skips the feed's GET and HEAD
// (app.Get registers both — fiber's csrf.New mints its safe-method cookie for
// either), does NOT skip a mutating verb under the same path prefix, and does
// not widen past the route's own shape to a path merely sharing its prefix.
func TestCSRFPredicateSkipsTheCookielessFeedGETAndHEADWithoutWideningTheMutatingExemptions(t *testing.T) {
	next := csrfMiddlewareConfig(false, newRateLimitTestHandler(t)).Next
	if next == nil {
		t.Fatal("csrfMiddlewareConfig.Next is nil")
	}

	// The shipped fiberConfig, not a bare fiber.New(): CaseSensitive and
	// StrictRouting are both off there, and this predicate has to agree with
	// the router's own normalization of the probed spellings, not a
	// coincidentally-matching default.
	probe := fiber.New(fiberConfig(proxySettings{}))
	probe.Use(func(c fiber.Ctx) error {
		if next(c) {
			return c.SendStatus(fiber.StatusTeapot)
		}
		return c.SendStatus(fiber.StatusOK)
	})
	isExempt := func(method string, path string) bool {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(""))
		response, err := probe.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("probe %s %s: %v", method, path, err)
		}
		defer func() { _ = response.Body.Close() }()
		return response.StatusCode == fiber.StatusTeapot
	}

	const feedTarget = api.CalendarFeedRateLimitPrefix + "/ABCDEFGHJKLMNPQRSTUVWXYZ23456789ABCDEFGHJKLMNP12.ics"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if !isExempt(method, feedTarget) {
			t.Errorf("expected %s %s to skip the CSRF middleware (no token issuance on the cookieless feed)", method, feedTarget)
		}
	}

	// A mutating verb under the same prefix — hypothetical today, since
	// internal/api/routes.go registers no such route — must still be
	// validated. Widening the predicate's method check would silently exempt
	// one from CSRF the day it's added.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if isExempt(method, feedTarget) {
			t.Errorf("expected %s %s to stay CSRF-validated: the feed clause must be GET/HEAD-only", method, feedTarget)
		}
	}

	// Neighbouring paths must not be swept in by a loosened prefix check: one
	// that merely starts with "/calendar", one that continues every character
	// of the feed prefix without its separator, the bare prefix itself (with
	// and without a trailing slash) and a nested segment beneath it — none of
	// these name the feed's own route shape.
	for _, neighbour := range []string{
		"/calendar/day/2026-01-15",
		api.CalendarFeedRateLimitPrefix + "back",
		api.CalendarFeedRateLimitPrefix,
		api.CalendarFeedRateLimitPrefix + "/",
		api.CalendarFeedRateLimitPrefix + "/a/b.ics",
	} {
		if isExempt(http.MethodGet, neighbour) {
			t.Errorf("expected GET %s to stay outside the feed's CSRF skip — the prefix must not over-match", neighbour)
		}
	}
}

// TestCSRFDeniesEveryMutatingRouteWithoutToken is the behavioral half of the
// guard: with the production middleware chain mounted, a token-less request to
// EVERY state-mutating route must be refused with the CSRF 403 before any
// handler runs — except the sole exemption, which must instead reach its
// handler (the OIDC callback is protected by the sealed one-time state cookie
// and, lacking one, redirects to /login). This also fails if the CSRF
// middleware is ever unmounted from the composition root.
//
// Each refusal is also required to carry the mapped envelope. The refusal is
// raised once, by the middleware, for every route in the table, so this is the
// route-wide sweep behind the app-wide envelope contract: a regression that
// restores the framework's bare "Forbidden" fails here on every mutating route
// rather than on whichever one a hand-written test happened to name.
func TestCSRFDeniesEveryMutatingRouteWithoutToken(t *testing.T) {
	app := newCSRFGuardTestApp(t)

	covered := 0
	for key, probePath := range mutatingRoutePathsByKey(t, app) {
		method := strings.SplitN(key, " ", 2)[0]
		_, exempt := csrfGuardExpectedExemptions[key]

		t.Run(key, func(t *testing.T) {
			request := httptest.NewRequest(method, probePath, strings.NewReader(""))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", "application/json")

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("request %s %s: %v", method, probePath, err)
			}
			defer func() { _ = response.Body.Close() }()

			if exempt {
				if response.StatusCode == http.StatusForbidden {
					t.Fatalf("exempted route %s must bypass CSRF validation and reach its handler, got 403", key)
				}
				if response.StatusCode >= http.StatusInternalServerError {
					t.Fatalf("exempted route %s returned %d; expected a handled (non-5xx) response", key, response.StatusCode)
				}
				return
			}
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("expected CSRF 403 for token-less %s, got %d — if this route was consciously exempted, update csrfGuardExpectedExemptions and SECURITY.md together", key, response.StatusCode)
			}
			body := mustReadAll(t, response)
			if strings.TrimSpace(string(body)) == "Forbidden" {
				t.Fatalf("%s answered the CSRF refusal with fiber's bare text; the mapped envelope is app-wide", key)
			}
			assertTransportErrorEnvelope(t, body, "forbidden", "forbidden")
		})
		covered++
	}

	if covered < 2 {
		t.Fatalf("expected the CSRF matrix to cover multiple mutating routes, covered %d", covered)
	}
}
