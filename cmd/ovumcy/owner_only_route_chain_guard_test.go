package main

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// v1PathPrefix is the mount point of the versioned JSON API (the group opened
// in internal/api/routes.go), and the scope of the invariant enforced here.
const v1PathPrefix = "/api/v1/"

// This file is the OwnerOnly half of the "endpoint defense-in-depth" invariant;
// csrf_exemption_guard_test.go in this package is its CSRF half, and both walk
// the same real route table built by the production composition root.
//
// The claim being enforced: every state-mutating /api/v1/* endpoint declares
// handler.OwnerOnly explicitly. It is checked against the ROUTE TABLE, not
// against a response, because the product has no second role to probe with:
// AuthRequired rejects an unsupported-role cookie itself, with the same 403,
// before OwnerOnly would ever run. A transport probe therefore cannot tell an
// endpoint that chains OwnerOnly from one that forgot it — which is exactly
// what internal/api/owner_only_coverage_regression_test.go says about its own
// (weaker, and complementary) contract.
//
// Which routes are exempt is not restated here. A mutation is in scope when the
// route table puts it behind AuthRequired, and the pre-auth endpoints (register,
// login, the 2FA challenge, the password-reset pair) are exempt because the same
// table shows they carry no AuthRequired at all. That set is reviewed where it
// already was — the publicRoutes map of the internal/api role matrix, which
// reddens if a new /api/v1 mutation appears outside AuthRequired — so a new
// authenticated endpoint here inherits the requirement instead of needing a
// list entry, and no hand-kept exclusion list can drift out of step with the
// registrations.

// routeChainKey identifies one entry of the Fiber route stack. Method and path
// alone are ambiguous — a group's middleware mount and a leaf route registered
// on the group's own prefix share both — so the handler chain is part of the
// key.
func routeChainKey(route fiber.Route) string {
	return route.Method + " " + route.Path + " " + strings.Join(routeHandlerNames(route), ",")
}

// routeHandlerNames resolves each handler in a route's chain to the fully
// qualified name of the function behind it. A method value such as
// handler.OwnerOnly compiles to a shared per-method wrapper, so every
// registration of the same method resolves to the same name.
func routeHandlerNames(route fiber.Route) []string {
	names := make([]string, 0, len(route.Handlers))
	for _, h := range route.Handlers {
		names = append(names, runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name())
	}
	return names
}

// middlewareFuncName is the same resolution applied to a middleware method
// value taken directly from the production handler, so a rename of the method
// moves the expectation with it instead of leaving a stale string literal.
func middlewareFuncName(t *testing.T, middleware fiber.Handler) string {
	t.Helper()

	name := runtime.FuncForPC(reflect.ValueOf(middleware).Pointer()).Name()
	if strings.TrimSpace(name) == "" {
		t.Fatal("could not resolve a middleware function name: every chain comparison in this guard would then match vacuously")
	}
	return name
}

// prefixMounts returns the route-stack entries that are middleware mounts
// (app.Use / a Group's own middleware) rather than endpoints: exactly the
// entries GetRoutes reports in full but omits when asked to filter them out.
// A group's middleware does not appear in the chains of the routes underneath
// it, so covering it needs this second list — routes.go already registers
// OwnerOnly on a whole group (the /api/v1/exports group), and a mutation added
// to such a group is protected without naming OwnerOnly on its own line.
func prefixMounts(app *fiber.App) []fiber.Route {
	endpoints := map[string]struct{}{}
	for _, route := range app.GetRoutes(true) {
		endpoints[routeChainKey(route)] = struct{}{}
	}

	mounts := []fiber.Route{}
	for _, route := range app.GetRoutes() {
		if _, isEndpoint := endpoints[routeChainKey(route)]; isEndpoint {
			continue
		}
		mounts = append(mounts, route)
	}
	return mounts
}

// chainContains reports whether the middleware named funcName runs for route:
// either on the route's own chain, or on a middleware mount that covers the
// route's path for the same method.
func chainContains(route fiber.Route, mounts []fiber.Route, funcName string) bool {
	for _, name := range routeHandlerNames(route) {
		if name == funcName {
			return true
		}
	}
	for _, mount := range mounts {
		if mount.Method != route.Method || !coversPath(mount.Path, route.Path) {
			continue
		}
		for _, name := range routeHandlerNames(mount) {
			if name == funcName {
				return true
			}
		}
	}
	return false
}

// coversPath reports whether a middleware mounted at mountPath runs for
// routePath — Fiber's USE matching is on whole path segments, so "/api/v1/day"
// does not cover "/api/v1/days".
func coversPath(mountPath string, routePath string) bool {
	if mountPath == "/" || mountPath == routePath {
		return true
	}
	return strings.HasPrefix(routePath, strings.TrimSuffix(mountPath, "/")+"/")
}

// TestEveryAuthenticatedV1MutationChainsOwnerOnly walks the real route table of
// the production composition root and asserts that every state-mutating
// /api/v1/* endpoint sitting behind AuthRequired also has handler.OwnerOnly in
// the chain that runs for it. Deleting OwnerOnly from a registration in
// routes.go fails here, naming the route.
func TestEveryAuthenticatedV1MutationChainsOwnerOnly(t *testing.T) {
	app := newCSRFGuardTestApp(t)
	handler := newRateLimitTestHandler(t)
	ownerOnly := middlewareFuncName(t, handler.OwnerOnly)
	authRequired := middlewareFuncName(t, handler.AuthRequired)
	if !strings.Contains(ownerOnly, "OwnerOnly") || !strings.Contains(authRequired, "AuthRequired") {
		t.Fatalf("resolved middleware names do not name their middleware (OwnerOnly=%q, AuthRequired=%q); the chain comparison below would be meaningless", ownerOnly, authRequired)
	}

	mutating := csrfMutatingMethods()
	mounts := prefixMounts(app)

	authenticated := 0
	preAuth := []string{}
	for _, route := range app.GetRoutes(true) {
		if _, isMutating := mutating[route.Method]; !isMutating {
			continue
		}
		if !strings.HasPrefix(route.Path, v1PathPrefix) {
			continue
		}
		key := route.Method + " " + route.Path
		if !chainContains(route, mounts, authRequired) {
			// A pre-auth endpoint: no session exists yet, so there is no role to
			// enforce. The reviewed list of these lives with the internal/api
			// role matrix; it is collected here only as this guard's negative
			// anchor.
			preAuth = append(preAuth, key)
			continue
		}

		t.Run(key, func(t *testing.T) {
			if !chainContains(route, mounts, ownerOnly) {
				t.Fatalf("%s is a state-mutating /api/v1 endpoint behind AuthRequired but never runs %s: every such endpoint declares handler.OwnerOnly explicitly (defense in depth — AuthRequired alone answers the same 403 for the only role this product has, so no response probe can see the difference)", key, ownerOnly)
			}
		})
		authenticated++
	}

	// Both anchors are needed: a broken AuthRequired probe that matches nothing
	// empties the checked set, and one that matches everything empties the
	// pre-auth set — either way the loop above would assert nothing while
	// staying green.
	if authenticated == 0 {
		t.Fatal("no authenticated state-mutating /api/v1 route was checked; recheck route discovery and the AuthRequired chain probe")
	}
	if len(preAuth) == 0 {
		t.Fatal("every state-mutating /api/v1 route looked authenticated; the AuthRequired chain probe is matching indiscriminately and the OwnerOnly check above proves nothing")
	}

	// The pre-auth endpoints are the negative anchor for the OwnerOnly probe
	// itself: they must be reported as NOT chaining OwnerOnly, or the probe is
	// stuck at true and would pass for a route that dropped it.
	for _, key := range preAuth {
		method, path, _ := strings.Cut(key, " ")
		for _, route := range app.GetRoutes(true) {
			if route.Method != method || route.Path != path {
				continue
			}
			if chainContains(route, mounts, ownerOnly) {
				t.Fatalf("%s carries no AuthRequired yet reports OwnerOnly in its chain; the chain probe cannot distinguish the two states", key)
			}
		}
	}
}
