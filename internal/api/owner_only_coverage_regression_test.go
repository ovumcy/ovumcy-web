package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUnsupportedRoleRejectedAcrossEveryAuthedV1Route is a forward-looking
// defense-in-depth matrix: it iterates every route registered on the Fiber app,
// filters down to /api/v1/* mutations that should require an authenticated
// owner, and asserts that an `ovumcy_auth` cookie issued for an unsupported
// (legacy partner) role is rejected on each one. New state-mutating endpoints
// inherit this coverage automatically; an explicit exclusion list documents the
// public auth flows that intentionally accept anonymous traffic.
//
// The contract is: AuthRequired must reject unsupported roles before any
// handler runs, even if the route forgets to add handler.OwnerOnly. Combined
// with the explicit OwnerOnly middleware on every mutation, this gives two
// independent layers of role enforcement.
func TestUnsupportedRoleRejectedAcrossEveryAuthedV1Route(t *testing.T) {
	t.Parallel()

	publicAPIRoutes := map[string]struct{}{
		"POST /api/v1/users":                  {},
		"POST /api/v1/sessions":               {},
		"POST /api/v1/sessions/2fa-challenge": {},
		"POST /api/v1/password-resets":        {},
		"POST /api/v1/password-resets/redeem": {},
	}

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "owner-only-coverage@example.com", "StrongPass1", true)
	if err := database.Model(&user).Update("role", "partner").Error; err != nil {
		t.Fatalf("set unsupported legacy role: %v", err)
	}
	user.Role = "partner"
	authCookie := issueAuthCookieForUser(t, user)

	covered := 0
	for _, route := range app.GetRoutes() {
		if !strings.HasPrefix(route.Path, "/api/v1/") {
			continue
		}
		if route.Method == http.MethodHead {
			continue
		}
		key := route.Method + " " + route.Path
		if _, isPublic := publicAPIRoutes[key]; isPublic {
			continue
		}

		path := concreteRoutePathForUnsupportedRoleProbe(route.Path)
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			request := httptest.NewRequest(route.Method, path, strings.NewReader(""))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", "application/json")
			request.Header.Set("Cookie", authCookie)

			response := mustAppResponse(t, app, request)
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("expected 403 for unsupported role on %s %s, got %d", route.Method, route.Path, response.StatusCode)
			}
			cleared := responseCookie(response.Cookies(), authCookieName)
			if cleared == nil || strings.TrimSpace(cleared.Value) != "" {
				t.Fatalf("expected unsupported-role denial to clear auth cookie on %s %s, got %#v", route.Method, route.Path, cleared)
			}
		})
		covered++
	}

	if covered == 0 {
		t.Fatal("expected at least one /api/v1/* route to be covered by the unsupported-role matrix; recheck route discovery")
	}
}

func concreteRoutePathForUnsupportedRoleProbe(routePath string) string {
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
