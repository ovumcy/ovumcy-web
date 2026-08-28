package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// preSessionV1Mutations is the exclusion set of the "every state-mutating
// /api/v1/* endpoint chains handler.OwnerOnly" invariant: the endpoints that run
// BEFORE any session exists, so there is no session role for OwnerOnly to
// enforce and routes.go registers them without AuthRequired.
//
// It is the only place in code the set is written down. The publicRoutes map of
// the role matrix below is built from it rather than repeating it, and
// TestPreSessionV1MutationExclusionsMatchTheSecurityInvariantsDocument pins it
// against the exclusion bullet of docs/SECURITY_INVARIANTS.md in both
// directions. That is what stops a sixth entry from being added here — a diff
// that reads like a test-fixture edit while it narrows a documented security
// invariant — without the tracked document naming it.
var preSessionV1Mutations = []string{
	"POST /api/v1/users",
	"POST /api/v1/sessions",
	"POST /api/v1/sessions/2fa-challenge",
	"POST /api/v1/password-resets",
	"POST /api/v1/password-resets/redeem",
}

// TestUnsupportedRoleRejectedAcrossEveryAuthedV1Route is a forward-looking
// defense-in-depth matrix: it iterates every route registered on the Fiber app
// — both /api/v1/* JSON routes and server-rendered page routes — and asserts
// that an `ovumcy_auth` cookie issued for an unsupported (legacy partner) role
// is rejected on each one requiring authentication. New endpoints inherit this
// coverage automatically; an explicit exclusion list documents the public
// auth/page flows that intentionally accept anonymous traffic.
//
// The contract is: AuthRequired must reject unsupported roles before any
// handler runs, even if the route forgets to add handler.OwnerOnly. Combined
// with the explicit OwnerOnly middleware on every mutation, this gives two
// independent layers of role enforcement.
//
// Only the first of those two layers is observable here, and deliberately so:
// AuthRequired short-circuits an unsupported-role cookie with the 403 this test
// compares against, before OwnerOnly would run, so removing OwnerOnly from a
// route changes neither the status nor the cleared cookie. Nothing about the
// second layer can be concluded from this file. The layer it does not see —
// that every state-mutating /api/v1 route behind AuthRequired declares
// handler.OwnerOnly — is enforced against the route table itself by
// TestEveryAuthenticatedV1MutationChainsOwnerOnly
// (cmd/ovumcy/owner_only_route_chain_guard_test.go).
//
// The publicRoutes map below is also the reviewed answer to "which endpoints
// take anonymous traffic": a new /api/v1 mutation registered outside
// AuthRequired reddens this matrix until it is listed here, which is what lets
// the route-table guard derive its own scope from the table instead of keeping
// a second exclusion list that could drift out of step with this one.
func TestUnsupportedRoleRejectedAcrossEveryAuthedV1Route(t *testing.T) {
	t.Parallel()

	publicRoutes := map[string]struct{}{
		"GET /healthz":     {},
		"GET /readyz":      {},
		"GET /favicon.ico": {},
		// The public language switch has to work for a visitor with no session —
		// the login and onboarding pages are where it is used — so it carries
		// neither AuthRequired nor OwnerOnly and cannot answer 403 for a cookie
		// naming an unsupported role. It is not therefore outside the role model:
		// its one state-changing effect on an account, storing the chosen language,
		// applies the same `services.IsOwnerUser` predicate OwnerOnly enforces,
		// inside the handler where the session is resolved. An unsupported-role
		// cookie changes nothing and gets the ordinary answer; that is pinned by
		// TestUnsupportedRoleLanguageSwitchStoresNothing
		// (account_language_regressions_test.go), not by this cookie-role matrix.
		"POST /lang":                          {},
		"GET /login":                          {},
		"GET /auth/oidc/start":                {},
		"GET " + oidcLogoutBridgePath:         {},
		"GET " + oidcLogoutBridgeRedirectPath: {},
		"GET /register":                       {},
		"GET /register/welcome":               {},
		"GET /recovery-code":                  {},
		"GET /forgot-password":                {},
		"GET /reset-password":                 {},
		"GET /auth/2fa":                       {},
		"POST /auth/oidc/callback":            {},
		"GET " + oidcLinkConfirmPath:          {},
		"POST " + oidcLinkConfirmPath:         {},
		"GET /privacy":                        {},
		// The calendar (.ics) feed authenticates by the PATH TOKEN alone — a
		// calendar client sends no cookie — so it is intentionally NOT behind
		// AuthRequired/OwnerOnly and never inspects the role. An unsupported-role
		// cookie is simply ignored; every invalid/absent-credential case (here, a
		// bogus token) returns the same bare 404-no-oracle, not a 403. Its owner
		// scoping is proven by the token-resolution + cross-user isolation tests in
		// internal/services (calendar_feed_service_test.go), not by this cookie-role
		// matrix.
		"GET " + calendarFeedRoutePath: {},
	}
	// The /api/v1 half of the allowlist is not spelled out here: it is the
	// documented pre-session exclusion set, read from its single declaration so
	// this map and docs/SECURITY_INVARIANTS.md cannot drift apart.
	for _, key := range preSessionV1Mutations {
		publicRoutes[key] = struct{}{}
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
		if route.Method == http.MethodHead {
			continue
		}
		if route.Path == "/" && route.Method != http.MethodGet {
			// The app-wide app.Use(handler.NotFound) catch-all (no path given)
			// registers under Fiber's root "/" node for every HTTP method; only
			// GET / is an actual page route (ShowDashboard). These synthetic
			// entries never reach AuthRequired, so they are not real endpoints.
			continue
		}
		key := route.Method + " " + route.Path
		if _, isPublic := publicRoutes[key]; isPublic {
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
		t.Fatal("expected at least one authenticated route to be covered by the unsupported-role matrix; recheck route discovery")
	}
}

// securityInvariantsDocPath is the tracked public mirror of the invariants,
// relative to this package's directory (Go runs a test with its own package
// directory as the working directory).
const securityInvariantsDocPath = "../../docs/SECURITY_INVARIANTS.md"

// preSessionExclusionAnchor is the opening of the bullet in
// docs/SECURITY_INVARIANTS.md that carries the exclusion set of the OwnerOnly
// invariant. It is matched as a literal so that deleting or rewording the
// bullet reddens loudly instead of silently emptying the documented set.
const preSessionExclusionAnchor = "The exclusion set is exactly the pre-session endpoints"

// documentedV1MutationPattern picks the backticked `METHOD /api/v1/...`
// citations out of that bullet.
var documentedV1MutationPattern = regexp.MustCompile("`((?:GET|HEAD|POST|PUT|PATCH|DELETE) /api/v1/[^`]*)`")

// TestPreSessionV1MutationExclusionsMatchTheSecurityInvariantsDocument is the
// consistency half of the OwnerOnly invariant: the rule lives in a tracked
// document and its exceptions live in preSessionV1Mutations, and this pins the
// two to the same set in BOTH directions. Adding a sixth /api/v1 mutation to
// the Go list fails here until the document names it; dropping one from the
// document fails here too.
//
// It deliberately compares against the document's own words rather than
// re-deriving the set from the route table: a route-table derivation would
// agree with routes.go by construction and would say nothing about whether the
// invariant a human reads still matches what the code excludes.
func TestPreSessionV1MutationExclusionsMatchTheSecurityInvariantsDocument(t *testing.T) {
	t.Parallel()

	if len(preSessionV1Mutations) == 0 {
		t.Fatal("preSessionV1Mutations is empty: the comparison below would pass by having nothing to compare")
	}

	raw, err := os.ReadFile(securityInvariantsDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", securityInvariantsDocPath, err)
	}

	bullet, ok := preSessionExclusionBullet(string(raw))
	if !ok {
		t.Fatalf("%s no longer contains a bullet opening with %q: the exclusions of the OwnerOnly invariant would then be recorded only in preSessionV1Mutations, which is exactly the drift this guard exists to prevent", securityInvariantsDocPath, preSessionExclusionAnchor)
	}

	documented := map[string]struct{}{}
	for _, match := range documentedV1MutationPattern.FindAllStringSubmatch(bullet, -1) {
		documented[match[1]] = struct{}{}
	}

	inCode := map[string]struct{}{}
	for _, key := range preSessionV1Mutations {
		inCode[key] = struct{}{}
	}

	for key := range inCode {
		if _, named := documented[key]; !named {
			t.Errorf("%s is excluded from the OwnerOnly invariant by preSessionV1Mutations but is not named in the exclusion bullet of %s: a security invariant may not be narrowed in a test literal alone", key, securityInvariantsDocPath)
		}
	}
	for key := range documented {
		if _, excluded := inCode[key]; !excluded {
			t.Errorf("%s is named as an exclusion in %s but is not in preSessionV1Mutations: the document claims an exception the role matrix does not make", key, securityInvariantsDocPath)
		}
	}
	if t.Failed() {
		t.Logf("documented: %v", sortedRouteKeys(documented))
		t.Logf("in code:    %v", sortedRouteKeys(inCode))
	}
}

// preSessionExclusionBullet returns the text of the markdown bullet that opens
// with preSessionExclusionAnchor, bounded by the next bullet or the end of the
// list block so a neighbouring bullet's `/api/v1/...` citations cannot leak in.
func preSessionExclusionBullet(document string) (string, bool) {
	start := strings.Index(document, preSessionExclusionAnchor)
	if start < 0 {
		return "", false
	}
	bullet := document[start:]
	end := len(bullet)
	for _, boundary := range []string{"\n- ", "\n\n"} {
		if index := strings.Index(bullet, boundary); index >= 0 && index < end {
			end = index
		}
	}
	return bullet[:end], true
}

func sortedRouteKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
