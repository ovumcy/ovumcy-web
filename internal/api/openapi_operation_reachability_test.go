package api

import (
	"fmt"
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
// internal/api or internal/services (excluding _test.go) to a bounded depth. A
// handler that never calls a status-bearing function reachable within that
// depth is undercounted rather than over-claimed — the same conservative-on-
// false-positives choice the removed rule-of-thumb in governance-update's own
// admission test asks for: a check that can go red on nothing is worse than one
// that occasionally stays quiet.
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
	funcs, transport := parseReachableFuncs(t,
		filepath.Join(repoRoot, "internal", "api"),
		filepath.Join(repoRoot, "internal", "services"))

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

		reach := newStatusReach(funcs, transport, statusByIdentifier)
		for _, h := range route.Handlers {
			name := handlerFuncName(h)
			for _, decl := range funcs[name] {
				// A handler value is always a transport function; resolving the
				// bare name against the domain package too would let a
				// same-named service method stand in for the real handler.
				if transport[decl] {
					reach.walkFunc(decl, 0, nil)
				}
			}
		}

		var missing []int
		for status := range reach.emittable() {
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

// parseReachableFuncs parses every non-test .go file directly under
// transportDir and domainDir, indexes each top-level func and each method by
// its bare name, and reports which declarations came from transportDir.
//
// A name is keyed to a SLICE, not a single decl: these packages have real
// same-name collisions across distinct receivers (validAt on five different
// sealed-cookie payload types, matchesState on two), and a bare-map index that
// overwrote on collision would silently drop whichever declaration parsed
// last — the wrong direction for a check whose whole point is not
// under-claiming what a name can reach. statusReach walks every decl a name
// resolves to.
//
// domainDir is walked for one purpose only: deciding which arm of a shared
// error mapper a given handler can actually reach (see statusReach). It
// contributes no statuses of its own — the transport set gates that, and the
// architecture contract already keeps internal/services free of any HTTP
// framework.
func parseReachableFuncs(t *testing.T, transportDir string, domainDir string) (map[string][]*ast.FuncDecl, map[*ast.FuncDecl]bool) {
	t.Helper()
	fset := token.NewFileSet()
	funcs := make(map[string][]*ast.FuncDecl)
	transport := make(map[*ast.FuncDecl]bool)

	for _, dir := range []string{transportDir, domainDir} {
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
				funcs[fn.Name.Name] = append(funcs[fn.Name.Name], fn)
				if dir == transportDir {
					transport[fn] = true
				}
			}
		}
	}
	if len(transport) == 0 {
		t.Fatalf("no functions parsed from %s; test setup is wrong", transportDir)
	}
	return funcs, transport
}

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

// guardSet is one case clause's sentinel errors, read as a disjunction: the arm
// runs if the mapped error matches ANY of them. A status carries a conjunction
// of those sets — one per clause it sits inside.
type guardSet []string

// statusReach walks one route's handler chain and answers which fiber.Status*
// values that chain can emit.
//
// Two narrowings keep it from claiming statuses the JSON contract can never
// show. Both drop statuses rather than add them, so they can only make the
// check quieter, never falsely red:
//
//   - Content negotiation. A status emitted only inside an isHTMX(c) or
//     !acceptsJSON(c) arm belongs to the HTML surface, which the spec's own
//     preamble puts outside this contract — the same argument crossCuttingStatus
//     makes for 303, applied where it lives instead of per status code. The
//     shared error responder answers HTMX with 200 and error markup, and four
//     handlers answer it with 204; neither is a JSON outcome.
//   - Shared error mappers. A status inside `case errors.Is(err, ErrX):` is
//     credited only when ErrX is producible somewhere in the same chain. Two
//     handlers share mapDayUpsertError but only the cycle-start one can raise
//     the conflict its 409 arm maps, and PrepareLocalPasswordHash cannot raise
//     the re-auth rate limit its mapper's 429 arm maps — call-graph reachability
//     of the MAPPER is not reachability of the ARM. Deciding that needs the
//     sentinel's producer, which is why the walk reads internal/services too.
//
// An `if errors.Is(...)` guard is deliberately NOT read as a narrowing, only a
// case clause is: the same reasoning would apply, but the one-directional design
// prefers keeping a status it cannot rule out over dropping one it can.
type statusReach struct {
	funcs              map[string][]*ast.FuncDecl
	transport          map[*ast.FuncDecl]bool
	statusByIdentifier map[string]int
	// visited is keyed by declaration pointer AND the guards in force, not by
	// name: two distinct declarations can share a bare name (parseReachableFuncs'
	// own doc comment has the confirmed collisions), and a name-keyed set would
	// mark the whole name walked after the first same-named decl. The guards
	// belong in the key because the same helper is reached both inside a
	// sentinel-guarded arm and outside one, and the unguarded sighting must not
	// be lost to a guarded visit that happened to come first.
	visited   map[string]bool
	sightings map[int]map[string][]guardSet
	sentinels map[string]bool
}

func newStatusReach(funcs map[string][]*ast.FuncDecl, transport map[*ast.FuncDecl]bool, statusByIdentifier map[string]int) *statusReach {
	return &statusReach{
		funcs:              funcs,
		transport:          transport,
		statusByIdentifier: statusByIdentifier,
		visited:            make(map[string]bool),
		sightings:          make(map[int]map[string][]guardSet),
		sentinels:          make(map[string]bool),
	}
}

func (reach *statusReach) walkFunc(decl *ast.FuncDecl, depth int, guards []guardSet) {
	if decl == nil || decl.Body == nil || depth > maxReachDepth {
		return
	}
	key := fmt.Sprintf("%p|%s", decl, guardKey(guards))
	if reach.visited[key] {
		return
	}
	reach.visited[key] = true
	reach.walk(decl.Body, reach.transport[decl], depth, guards)
}

func (reach *statusReach) walk(node ast.Node, emitsStatuses bool, depth int, guards []guardSet) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch typed := n.(type) {
		case *ast.IfStmt:
			if !nonJSONSurfaceGuard(typed.Cond) {
				return true
			}
			if typed.Else != nil {
				reach.walk(typed.Else, emitsStatuses, depth, guards)
			}
			return false
		case *ast.CaseClause:
			sentinels := caseGuardSentinels(typed.List)
			if len(sentinels) == 0 {
				return true
			}
			inner := append(append([]guardSet{}, guards...), sentinels)
			for _, stmt := range typed.Body {
				reach.walk(stmt, emitsStatuses, depth, inner)
			}
			return false
		case *ast.SelectorExpr:
			if pkg, ok := typed.X.(*ast.Ident); ok && pkg.Name == "fiber" && emitsStatuses {
				if status, ok := reach.statusByIdentifier["fiber."+typed.Sel.Name]; ok {
					reach.record(status, guards)
				}
			}
			return true
		case *ast.Ident:
			if name := sentinelName(typed); name != "" {
				reach.sentinels[name] = true
			}
			return true
		case *ast.CallExpr:
			// The sentinel named in a guard is being TESTED, not produced;
			// harvesting it here would make every mapper arm self-satisfying.
			if isSentinelComparison(typed) {
				return false
			}
			for _, callee := range reach.funcs[calleeName(typed)] {
				reach.walkFunc(callee, depth+1, guards)
			}
			return true
		}
		return true
	})
}

func (reach *statusReach) record(status int, guards []guardSet) {
	if reach.sightings[status] == nil {
		reach.sightings[status] = make(map[string][]guardSet)
	}
	reach.sightings[status][guardKey(guards)] = guards
}

// emittable keeps a status when at least one of its sightings sits under guards
// the chain can satisfy. An unguarded sighting always counts.
func (reach *statusReach) emittable() map[int]bool {
	out := make(map[int]bool)
	for status, byGuard := range reach.sightings {
		for _, guards := range byGuard {
			if reach.satisfiable(guards) {
				out[status] = true
				break
			}
		}
	}
	return out
}

func (reach *statusReach) satisfiable(guards []guardSet) bool {
	for _, set := range guards {
		matched := false
		for _, name := range set {
			if reach.sentinels[name] {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func guardKey(guards []guardSet) string {
	if len(guards) == 0 {
		return ""
	}
	parts := make([]string, len(guards))
	for i, set := range guards {
		parts[i] = strings.Join(set, "|")
	}
	return strings.Join(parts, "&")
}

// nonJSONSurfaceGuard reports whether an if-condition selects the HTML surface.
// Only top-level conjuncts are read: `acceptsJSON(c) || isHTMX(c)` names the
// union of both surfaces and must not be taken for HTML-only.
func nonJSONSurfaceGuard(cond ast.Expr) bool {
	for _, term := range conjuncts(cond) {
		switch typed := term.(type) {
		case *ast.CallExpr:
			if calleeName(typed) == "isHTMX" {
				return true
			}
		case *ast.UnaryExpr:
			if typed.Op != token.NOT {
				continue
			}
			if call, ok := unparen(typed.X).(*ast.CallExpr); ok && calleeName(call) == "acceptsJSON" {
				return true
			}
		}
	}
	return false
}

// caseGuardSentinels returns the sentinel errors a case clause tests, or nil
// when the clause is anything other than a list of errors.Is/errors.As calls
// naming Err* values — a `default:` arm, a tagged switch, or a mixed condition
// stays unguarded, which is the direction that keeps statuses.
func caseGuardSentinels(list []ast.Expr) guardSet {
	if len(list) == 0 {
		return nil
	}
	var names guardSet
	for _, expr := range list {
		call, ok := unparen(expr).(*ast.CallExpr)
		if !ok || !isSentinelComparison(call) {
			return nil
		}
		for _, arg := range call.Args {
			if name := sentinelName(arg); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func isSentinelComparison(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "errors" && (selector.Sel.Name == "Is" || selector.Sel.Name == "As")
}

func sentinelName(expr ast.Expr) string {
	switch typed := unparen(expr).(type) {
	case *ast.Ident:
		if strings.HasPrefix(typed.Name, "Err") {
			return typed.Name
		}
	case *ast.SelectorExpr:
		if strings.HasPrefix(typed.Sel.Name, "Err") {
			return typed.Sel.Name
		}
	}
	return ""
}

func calleeName(call *ast.CallExpr) string {
	switch fn := unparen(call.Fun).(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

func conjuncts(expr ast.Expr) []ast.Expr {
	if binary, ok := unparen(expr).(*ast.BinaryExpr); ok && binary.Op == token.LAND {
		return append(conjuncts(binary.X), conjuncts(binary.Y)...)
	}
	return []ast.Expr{unparen(expr)}
}

func unparen(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
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
