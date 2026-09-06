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
// in internal/. Two real drifts of exactly that shape shipped for a release:
// four re-auth-budget endpoints undeclaring 429. The spec contract binds each
// operation separately — every operation declares every status it can emit.
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
		if isTransportHEADTwin(app, route) {
			// The twin dispatches the GET operation's own chain, so it emits
			// exactly what that operation emits and the spec declares those
			// statuses once, there. See isTransportHEADTwin.
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

	// A guard that reached no route and a guard that found nothing print the
	// same nothing. The floor is the population, not the verdict — and it is
	// read out of the spec rather than written down here, so it tracks the
	// document instead of dating from whenever someone last counted. The two
	// sets are the same operations by TestOpenAPIContractMatchesRegisteredRoutes,
	// which fails first and by name if they ever diverge; here their SIZE is all
	// that is asked, as the cheapest statement that a route table failing to
	// load cannot satisfy.
	specOperations := openAPIV1Routes(t, filepath.Join(repoRoot, "docs", "openapi.yaml"))
	if len(seen) < len(specOperations) {
		t.Fatalf("walked %d /api/v1 operations against %d in the spec; the route table did not load",
			len(seen), len(specOperations))
	}

	if len(offenders) == 0 {
		return
	}
	sort.Strings(offenders)
	t.Errorf("operations whose handler chain can emit a status docs/openapi.yaml never declares for that operation:\n  %s",
		strings.Join(offenders, "\n  "))
}

// TestStatusReachTellsARedirectJSONCallersGetFromOneTheyDoNot exercises the
// analyser itself, on source written for it. The sibling test above can only
// ever report what the tree happens to contain, so once 303 stopped being
// excluded wholesale its silence proved nothing: a narrowing that suppressed
// every redirect and a narrowing that suppressed the right ones read the same.
// Each case therefore carries a status the walk MUST still find, so a run that
// reached nothing cannot pass as a run that found nothing.
func TestStatusReachTellsARedirectJSONCallersGetFromOneTheyDoNot(t *testing.T) {
	const source = `package api

func redirectsEveryNonHTMXCaller(c fiber.Ctx) error {
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(nil)
	}
	if isHTMX(c) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
}

func answersJSONBeforeRedirecting(c fiber.Ctx) error {
	if isHTMX(c) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	if acceptsJSON(c) {
		return c.Status(fiber.StatusAccepted).JSON(nil)
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
}

func redirectsThroughTheSharedHelper(c fiber.Ctx) error {
	switch responseFormat(c) {
	case httpx.ResponseFormatJSON:
		return c.Status(fiber.StatusAccepted).JSON(nil)
	default:
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}
}

func redirectsThroughAnUnqualifiedHelper(c fiber.Ctx) error {
	switch responseFormat(c) {
	case ResponseFormatJSON:
		return c.Status(fiber.StatusAccepted).JSON(nil)
	default:
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}
}

func negotiatesAfterAnInitializer(c fiber.Ctx) error {
	switch spec := mapSomethingError(fiber.StatusConflict); responseFormat(c) {
	case httpx.ResponseFormatJSON:
		return c.Status(fiber.StatusAccepted).JSON(spec)
	default:
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}
}

func redirectsWhenTheJSONArmFallsThrough(c fiber.Ctx) error {
	if acceptsJSON(c) {
		c.Status(fiber.StatusAccepted)
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "reach_fixture.go", source, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	funcs := make(map[string][]*ast.FuncDecl)
	transport := make(map[*ast.FuncDecl]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		funcs[fn.Name.Name] = append(funcs[fn.Name.Name], fn)
		transport[fn] = true
	}

	statusByIdentifier := knownFiberStatusIdentifiers(t)

	for _, testCase := range []struct {
		name     string
		redirect bool
		floor    int
	}{
		// The bare fall-through: nothing between the HTMX arm and the redirect,
		// so a JSON caller lands on it. This is the shape both 2FA mutations had.
		{name: "redirectsEveryNonHTMXCaller", redirect: true, floor: http.StatusUnauthorized},
		// The JSON caller has already left; the redirect below is HTML-only.
		{name: "answersJSONBeforeRedirecting", redirect: false, floor: http.StatusAccepted},
		// Same question asked as a switch, which is how redirectOrJSON asks it.
		{name: "redirectsThroughTheSharedHelper", redirect: false, floor: http.StatusAccepted},
		// The same switch with the constant unqualified, as httpx would write
		// it. The floor is the JSON arm's own status: matching only the
		// qualified spelling skips that arm, which hides a JSON status rather
		// than an HTML one.
		{name: "redirectsThroughAnUnqualifiedHelper", redirect: false, floor: http.StatusAccepted},
		// The initializer runs before any arm is chosen, so its 409 is emittable
		// to a JSON caller and must survive the narrowing.
		{name: "negotiatesAfterAnInitializer", redirect: false, floor: http.StatusConflict},
		// An acceptsJSON arm that does NOT return decides nothing: execution
		// continues into the redirect for the JSON caller too.
		{name: "redirectsWhenTheJSONArmFallsThrough", redirect: true, floor: http.StatusAccepted},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			reach := newStatusReach(funcs, transport, statusByIdentifier)
			reach.walkFunc(funcs[testCase.name][0], 0, nil)
			emittable := reach.emittable()

			if !emittable[testCase.floor] {
				t.Fatalf("walk found no %d; it reached nothing, so its verdict on the redirect means nothing",
					testCase.floor)
			}
			if got := emittable[http.StatusSeeOther]; got != testCase.redirect {
				t.Errorf("sees the 303 = %v, want %v", got, testCase.redirect)
			}
		})
	}
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
// the ErrorHandler's total-resolution contract and
// TestOpenAPIDocumentsEveryTransportStatusTheEnvelopeCovers).
//
// 303 was listed here too until the analyser could tell the two kinds of
// redirect apart. The reason given was that any handler MAY reach the shared
// content-negotiation helper regardless of its own logic — true of the helper,
// and true of the shared error responder, but not of a handler that falls
// through to c.Redirect() with nothing answering the JSON caller first. Both
// 2FA mutations did exactly that and the exclusion hid it: `PUT` and
// `DELETE /api/v1/users/current/2fa` redirected an `Accept: application/json`
// client to a page, undeclared, on a green main. The exclusion is gone and the
// surface question is now answered where it is asked — see statusReach's third
// and fourth narrowings, which read an `acceptsJSON` early return and a
// `switch responseFormat(c)` arm. Removing it wholesale, without those two,
// reddened 20 operations of which 18 were false.
var crossCuttingStatus = map[int]bool{
	http.StatusInternalServerError: true,
}

// guardSet is one case clause's sentinel errors, read as a disjunction: the arm
// runs if the mapped error matches ANY of them. A status carries a conjunction
// of those sets — one per clause it sits inside.
type guardSet []string

// statusReach walks one route's handler chain and answers which fiber.Status*
// values that chain can emit.
//
// Four narrowings keep it from claiming statuses the JSON contract can never
// show. All of them drop statuses rather than add them, so they can only make
// the check quieter, never falsely red — which is also why each one is written
// to recognise a single exact spelling and fall through on anything else:
//
//   - Content negotiation, asked as a guard. A status emitted only inside an
//     isHTMX(c) or !acceptsJSON(c) arm belongs to the HTML surface, which the
//     spec's own preamble puts outside this contract. The shared error responder
//     answers HTMX with 200 and error markup, and four handlers answer it with
//     204; neither is a JSON outcome.
//   - Content negotiation, asked as an early return. `if acceptsJSON(c) { …
//     return … }` ends the JSON caller's request, so every LATER statement in
//     that block is the HTML surface too — the redirect at the bottom of a
//     settings mutation above all. This one needs statement ORDER, which is why
//     walk descends into a block by its List instead of letting ast.Inspect
//     flatten it, and it is what separates a handler that answers a JSON caller
//     from one that redirects it. Without it, 18 operations that answer JSON
//     perfectly well read as redirecting it.
//   - Content negotiation, asked as a switch. `switch responseFormat(c)` is the
//     same question again, and redirectOrJSON — reachable from most mutations —
//     puts c.JSON on its JSON arm and the 303 on its default one. Only the JSON
//     arm is walked.
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
		case *ast.BlockStmt:
			// Statements are walked in order rather than as an unordered tree,
			// because `if acceptsJSON(c) { return c.JSON(...) }` decides the
			// surface of everything AFTER it: a JSON caller has already left the
			// function, so the redirect at the bottom belongs to the HTML surface
			// as surely as one written inside an isHTMX arm. Reading only the
			// guard's own body — which is all an unordered walk can see — is what
			// made every redirect-answering handler look alike.
			for _, stmt := range typed.List {
				reach.walk(stmt, emitsStatuses, depth, guards)
				if jsonSurfaceEarlyReturn(stmt) {
					return false
				}
			}
			return false
		case *ast.SwitchStmt:
			// `switch responseFormat(c)` asks the same question the if-guards
			// ask, so its non-JSON arms are the same HTML surface: the shared
			// redirectOrJSON helper returns c.JSON on the JSON arm and the 303
			// only on the default one, which is why every handler that merely
			// CALLS it looked like a handler that redirects JSON callers.
			if typed.Tag == nil || !isResponseFormatCall(typed.Tag) {
				return true
			}
			// The initializer and the tag run before any arm is chosen, so they
			// run for every surface — the same reason the IfStmt arm below walks
			// its own Init and Cond rather than skipping the whole statement.
			if typed.Init != nil {
				reach.walk(typed.Init, emitsStatuses, depth, guards)
			}
			reach.walk(typed.Tag, emitsStatuses, depth, guards)
			for _, stmt := range typed.Body.List {
				clause, ok := stmt.(*ast.CaseClause)
				if !ok || !jsonFormatCase(clause.List) {
					continue
				}
				for _, inner := range clause.Body {
					reach.walk(inner, emitsStatuses, depth, guards)
				}
			}
			return false
		case *ast.IfStmt:
			if !nonJSONSurfaceGuard(typed.Cond) {
				return true
			}
			// Only the BODY is the HTML arm. The guard itself still runs on
			// every request, so its init and condition are walked like any
			// other code — skipping them would drop a status, or a sentinel a
			// later mapper arm depends on, that the JSON surface does reach.
			if typed.Init != nil {
				reach.walk(typed.Init, emitsStatuses, depth, guards)
			}
			reach.walk(typed.Cond, emitsStatuses, depth, guards)
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

// jsonSurfaceEarlyReturn reports whether a statement is `if acceptsJSON(c) {
// … return … }` — the shape that answers the JSON caller and leaves, making
// every later statement in the same block HTML-only.
//
// Three narrowings keep it from claiming an exit the JSON surface does not
// take, since being wrong here DROPS a status rather than adding one: the
// condition must be exactly the call (a conjunction like `acceptsJSON(c) && x`
// still falls through whenever `x` is false), the arm must not have an else
// (which would make the whole statement the branch, not an exit), and its body
// must end in a return. A `panic`, a labelled break or a return buried in a
// nested if is deliberately not read as terminal: an exit this cannot prove is
// one it keeps walking past, which is the direction that keeps statuses.
func jsonSurfaceEarlyReturn(stmt ast.Stmt) bool {
	ifStmt, ok := stmt.(*ast.IfStmt)
	if !ok || ifStmt.Else != nil || ifStmt.Init != nil || ifStmt.Body == nil {
		return false
	}
	call, ok := unparen(ifStmt.Cond).(*ast.CallExpr)
	if !ok || calleeName(call) != "acceptsJSON" {
		return false
	}
	if len(ifStmt.Body.List) == 0 {
		return false
	}
	_, terminal := ifStmt.Body.List[len(ifStmt.Body.List)-1].(*ast.ReturnStmt)
	return terminal
}

// isResponseFormatCall reports whether a switch tag is the content-negotiation
// question itself, `responseFormat(c)`. Only that spelling narrows: a switch on
// anything else is ordinary control flow whose arms all belong to every surface.
func isResponseFormatCall(tag ast.Expr) bool {
	call, ok := unparen(tag).(*ast.CallExpr)
	return ok && calleeName(call) == "responseFormat"
}

// jsonFormatCase reports whether a case clause of such a switch is its JSON
// arm. A `default:` clause has an empty list and is therefore never one — which
// is correct: default is where redirectOrJSON puts the browser redirect.
//
// The constant is matched by NAME, qualified or not. Every other narrowing here
// errs quiet because what it drops is the HTML surface; this one is the
// exception — dropping the JSON arm hides a status the contract does cover — so
// it must not be keyed on one spelling. `httpx.ResponseFormatJSON` is how the
// transport package writes it and `ResponseFormatJSON` is how httpx itself
// would.
func jsonFormatCase(list []ast.Expr) bool {
	for _, expr := range list {
		switch typed := unparen(expr).(type) {
		case *ast.SelectorExpr:
			if typed.Sel.Name == "ResponseFormatJSON" {
				return true
			}
		case *ast.Ident:
			if typed.Name == "ResponseFormatJSON" {
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
