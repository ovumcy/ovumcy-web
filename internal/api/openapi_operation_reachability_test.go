package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestOpenAPIOperationsDeclareEveryStatusTheirOwnHandlerChainCanEmit closes the
// direction TestOpenAPIDeclaresOnlyStatusesTheServerCanEmit's own doc comment
// names as underivable "from a source scan": that test proves declared ⊆
// emittable across the WHOLE server, which passes even when a status is
// emittable only by an operation that never declared it — a 429 the spec
// forgot is invisible to a check that only asks whether 429 appears anywhere
// in internal/. Two real drifts of exactly that shape shipped for a release
// (four re-auth-budget endpoints undeclaring 429; the "OpenAPI spec contract"
// rule in .claude/rules/api.md — governance-update, 2026-09-02 — has the
// prose).
//
// This test is deliberately one-directional, the same way its sibling is:
// under-declaration (emittable but not declared) is what it asserts, because
// over-declaration (declared but never reachable) needs proving a NEGATIVE —
// that no path through the handler chain reaches a status — which a call-graph
// approximation cannot soundly claim. Missing a declaration is provable by
// finding the one call site that emits it; the reverse is not provable by not
// finding one.
//
// "Emittable" is read from the registered handler chain for that exact route
// (Fiber's own Route.Handlers, not routes.go text), walked as Go AST rather
// than grepped: every fiber.Status* selector reached by a breadth-first walk
// from each handler function, following calls to functions/methods defined in
// internal/api (excluding _test.go) to a bounded depth. A handler that never
// calls a status-bearing function reachable within that depth is undercounted
// rather than over-claimed — the same conservative-on-false-positives choice
// the removed rule-of-thumb in governance-update's own admission test asks
// for: a check that can go red on nothing is worse than one that occasionally
// stays quiet.
func TestOpenAPIOperationsDeclareEveryStatusTheirOwnHandlerChainCanEmit(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	app, _ := newOnboardingTestApp(t)

	declared := openAPIDeclaredStatuses(t, filepath.Join(repoRoot, "docs", "openapi.yaml"))
	declaredByOperation := make(map[string]map[int]bool)
	for status, operations := range declared {
		for _, operation := range operations {
			if declaredByOperation[operation] == nil {
				declaredByOperation[operation] = make(map[int]bool)
			}
			declaredByOperation[operation][status] = true
		}
	}

	statusByIdentifier := knownFiberStatusIdentifiers(t)
	funcs := parseAPIPackageFuncs(t, filepath.Join(repoRoot, "internal", "api"))

	valid := map[string]bool{
		fiber.MethodGet: true, fiber.MethodPost: true, fiber.MethodPut: true,
		fiber.MethodPatch: true, fiber.MethodDelete: true, fiber.MethodHead: true,
	}

	var offenders []string
	seen := make(map[string]bool)
	for _, route := range app.GetRoutes(true) {
		if !valid[route.Method] || !strings.HasPrefix(route.Path, "/api/v1") {
			continue
		}
		operation := route.Method + " " + fiberPathToOpenAPI(route.Path)
		if seen[operation] {
			continue // duplicate registration (e.g. HEAD mirroring GET); one check is enough
		}
		seen[operation] = true

		emittable := make(map[int]bool)
		for _, h := range route.Handlers {
			name := handlerFuncName(h)
			if decl, ok := funcs[name]; ok {
				collectReachableStatuses(decl, funcs, statusByIdentifier, emittable, make(map[string]bool), 0)
			}
		}

		var missing []int
		for status := range emittable {
			if crossCuttingStatus[status] {
				continue
			}
			if !declaredByOperation[operation][status] {
				missing = append(missing, status)
			}
		}
		if len(missing) == 0 {
			continue
		}
		sort.Ints(missing)
		labels := make([]string, len(missing))
		for i, s := range missing {
			labels[i] = http.StatusText(s)
		}
		offenders = append(offenders, operation+": emits "+strings.Join(labels, ", ")+" but docs/openapi.yaml does not declare it there")
	}

	if len(offenders) == 0 {
		return
	}
	sort.Strings(offenders)
	t.Errorf("operations whose handler chain can emit a status docs/openapi.yaml never declares for that operation:\n  %s",
		strings.Join(offenders, "\n  "))
}

// handlerFuncName extracts the bare Go function/method name a fiber.Handler
// value was built from — "ChangePassword" out of the runtime symbol
// ".../internal/api.(*Handler).ChangePassword-fm" a bound method value carries,
// or the same bare name for a plain function. Handlers that are not simple
// named funcs/methods (an inline closure, a wrapped middleware factory) yield
// "" and are silently skipped: the walk is already one-directional, and a
// wrapper this cannot name is an underclaim, not a false one.
func handlerFuncName(h fiber.Handler) string {
	symbol := runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
	match := regexp.MustCompile(`\.(\w+)(?:-fm)?$`).FindStringSubmatch(symbol)
	if match == nil {
		return ""
	}
	return match[1]
}

// parseAPIPackageFuncs parses every non-test .go file directly under dir and
// indexes each top-level func and each method on *Handler by its bare name.
// Two distinct functions sharing a name (none do today) would merge into one
// entry — an over-approximation, which is the safe direction for a
// missing-declaration check.
func parseAPIPackageFuncs(t *testing.T, dir string) map[string]*ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	funcs := make(map[string]*ast.FuncDecl)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			funcs[fn.Name.Name] = fn
		}
	}
	if len(funcs) == 0 {
		t.Fatalf("no functions parsed from %s; test setup is wrong", dir)
	}
	return funcs
}

// collectReachableStatuses walks decl's body, recording every fiber.Status*
// selector it finds directly and recursing into every call to another
// function/method this package defines, up to maxReachDepth hops. visited
// guards both infinite recursion (mutual calls) and repeated work across the
// route's handler chain.
const maxReachDepth = 8

// crossCuttingStatus lists statuses excluded from the per-operation check
// because they are cross-cutting rather than a decision the operation's own
// business logic makes: 500 is the universal default arm of every domain
// error-mapping switch (already documented centrally, not per-operation, by
// api.md's ErrorHandler total-resolution rule and
// TestOpenAPIDocumentsEveryTransportStatusTheEnvelopeCovers); 303 is emitted
// by the shared JSON/HTMX content-negotiation helper any handler MAY reach
// regardless of its own logic, and the spec's own preamble scopes documented
// responses to the JSON surface. An operation that documents 303 as an actual
// primary outcome (the two-step password-reset flow) is unaffected — this
// exclusion only suppresses it as a MISSING-declaration finding, it does not
// forbid declaring it.
var crossCuttingStatus = map[int]bool{
	http.StatusInternalServerError: true,
	http.StatusSeeOther:            true,
}

func collectReachableStatuses(decl *ast.FuncDecl, funcs map[string]*ast.FuncDecl, statusByIdentifier map[string]int, out map[int]bool, visited map[string]bool, depth int) {
	if decl == nil || decl.Body == nil || visited[decl.Name.Name] || depth > maxReachDepth {
		return
	}
	visited[decl.Name.Name] = true

	ast.Inspect(decl.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := node.X.(*ast.Ident); ok && pkg.Name == "fiber" {
				if status, ok := statusByIdentifier["fiber."+node.Sel.Name]; ok {
					out[status] = true
				}
			}
		case *ast.CallExpr:
			switch fn := node.Fun.(type) {
			case *ast.Ident:
				if callee, ok := funcs[fn.Name]; ok {
					collectReachableStatuses(callee, funcs, statusByIdentifier, out, visited, depth+1)
				}
			case *ast.SelectorExpr:
				if callee, ok := funcs[fn.Sel.Name]; ok {
					collectReachableStatuses(callee, funcs, statusByIdentifier, out, visited, depth+1)
				}
			}
		}
		return true
	})
}

// knownFiberStatusIdentifiers inverts fiberStatusIdentifier over every
// registered HTTP status, so a selector found in source resolves back to its
// numeric code without a hand-maintained table that could drift from it.
func knownFiberStatusIdentifiers(t *testing.T) map[string]int {
	t.Helper()
	out := make(map[string]int)
	for status := 100; status < 600; status++ {
		if http.StatusText(status) == "" {
			continue
		}
		out[fiberStatusIdentifier(t, status)] = status
	}
	return out
}
