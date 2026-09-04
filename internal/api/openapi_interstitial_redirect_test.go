package api

import (
	"go/ast"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// interstitialHelper is the one function that turns a browser hop to the IdP
// into a same-origin 200 page. Reaching it from a route's handler chain is the
// positive witness this guard is built on.
const interstitialHelper = "oidcSameOriginRedirectInterstitial"

// TestOpenAPIOperationsServingAnInterstitialDeclareNoBrowserRedirect closes the
// one over-declaration this spec CAN decide without proving a negative.
//
// Over-declaration is generally undecidable here, which is why
// TestOpenAPIOperationsDeclareEveryStatusTheirOwnHandlerChainCanEmit is
// one-directional: "no path reaches this status" is not provable by a
// call-graph approximation failing to find one. This guard never asks that.
// It asks the opposite, positive question — does the chain reach
// oidcSameOriginRedirectInterstitial? — and a hit is a witness, not an absence.
//
// A hit settles the browser answer completely. The helper exists because CSP
// pins form-action to 'self' across a form navigation's whole redirect chain,
// so the cross-origin hop to the provider cannot be a 3xx at all; the operation
// hands the browser a 200 same-origin page whose meta-refresh performs it. An
// operation that serves it therefore has no browser redirect to declare, and
// the 303 the three OIDC step-ups published for two releases described a hop
// that had not existed since the CSP fix. The one status they really can
// redirect a browser with is respondSettingsError's refusal bounce to
// /settings, which every /api/v1/users/current mutation shares and none of the
// ~30 others declares — a cross-cutting HTML-surface answer the spec preamble
// puts outside this contract, not a per-operation outcome.
//
// Neither status guard could see this. The whole-server sweep passes because
// 303 is emitted all over internal/; the per-operation walk passes because 303
// really is reachable here, just not for the declared reason. The falsehood
// lived in the response DESCRIPTION, and nothing in this package reads one.
func TestOpenAPIOperationsServingAnInterstitialDeclareNoBrowserRedirect(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	app, _ := newOnboardingTestApp(t)

	declared := openAPIDeclaredStatuses(t, filepath.Join(repoRoot, "docs", "openapi.yaml"))
	declaresRedirect := make(map[string]bool)
	for _, operation := range declared[http.StatusSeeOther] {
		declaresRedirect[operation] = true
	}

	funcs, transport := parseReachableFuncs(t,
		filepath.Join(repoRoot, "internal", "api"),
		filepath.Join(repoRoot, "internal", "services"))

	valid := map[string]bool{
		fiber.MethodGet: true, fiber.MethodPost: true, fiber.MethodPut: true,
		fiber.MethodPatch: true, fiber.MethodDelete: true, fiber.MethodHead: true,
	}

	var serving, offenders []string
	seen := make(map[string]bool)
	for _, route := range app.GetRoutes(true) {
		if !valid[route.Method] || !strings.HasPrefix(route.Path, "/api/v1") {
			continue
		}
		operation := route.Method + " " + fiberPathToOpenAPI(route.Path)
		if seen[operation] {
			continue
		}
		seen[operation] = true

		reach := &interstitialReach{funcs: funcs, visited: make(map[*ast.FuncDecl]bool)}
		for _, h := range route.Handlers {
			for _, decl := range funcs[handlerFuncName(h)] {
				if transport[decl] {
					reach.walk(decl, 0)
				}
			}
		}
		if !reach.found {
			continue
		}
		serving = append(serving, operation)
		if declaresRedirect[operation] {
			offenders = append(offenders, operation)
		}
	}

	// The sweep means nothing if it found no interstitial at all: a rename of
	// the helper, or a walk that stopped descending, would leave it green
	// having checked nothing. Four OIDC step-ups serve one today.
	if len(serving) == 0 {
		t.Fatalf("no /api/v1 operation reaches %s; the helper was renamed or the walk stopped descending", interstitialHelper)
	}

	if len(offenders) == 0 {
		return
	}
	sort.Strings(offenders)
	t.Errorf("operations whose browser answer is a same-origin interstitial (200) still declare a 303 in docs/openapi.yaml:\n  %s",
		strings.Join(offenders, "\n  "))
}

// interstitialReach answers whether one route's handler chain can reach the
// interstitial helper. Unlike statusReach it needs no surface or sentinel
// narrowing: it looks for a single call by name, and a call it cannot resolve
// makes it quieter, never falsely red.
type interstitialReach struct {
	funcs   map[string][]*ast.FuncDecl
	visited map[*ast.FuncDecl]bool
	found   bool
}

func (reach *interstitialReach) walk(decl *ast.FuncDecl, depth int) {
	if reach.found || decl == nil || decl.Body == nil || depth > maxReachDepth || reach.visited[decl] {
		return
	}
	reach.visited[decl] = true
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return !reach.found
		}
		name := calleeName(call)
		if name == interstitialHelper {
			reach.found = true
			return false
		}
		for _, callee := range reach.funcs[name] {
			reach.walk(callee, depth+1)
		}
		return !reach.found
	})
}
