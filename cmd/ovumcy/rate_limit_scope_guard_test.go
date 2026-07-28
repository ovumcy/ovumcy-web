package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
)

// The limiters wired through rateLimitOnlyFor cap a single (method, path) each,
// because they are mounted with app.Use — which is prefix-matched and
// method-agnostic — and must not spill onto the neighbours sharing their
// prefix. That scope predicate compared c.Path() as raw bytes while the router
// matches a normalized path, so POST /LANG and POST /lang/ reached the language
// handler with the limiter's Next predicate waving them through: the language
// switch, the one unauthenticated body reader outside /api, lost its only cap on
// a change of case. Its four siblings shared the defect and kept the /api
// catch-all budget (mounted on a prefix, so fiber normalizes for it) plus the
// service-level AuthAttemptPolicy underneath — a broader budget, not none.
//
// The sweep below is deliberately not a list of the five: it reads the call
// sites out of the package source, so a sixth limiter added later is covered
// the moment it is wired, and a limiter whose arguments this sweep cannot read
// fails here instead of being skipped.

// scopedLimiterSpec is one rateLimitOnlyFor call site: the (method, path) pair
// a limiter is scoped to, plus where it is wired so a failure names it.
type scopedLimiterSpec struct {
	method string
	path   string
	source string
}

// scopedLimiterFloor guards against a vacuous pass. Discovery walks the package
// source, so a rename of rateLimitOnlyFor — or a parser that silently matches
// nothing — would otherwise sweep an empty set and report success. Five scoped
// limiters are wired today (logout, login, register, forgot-password, language
// switch); removing one is a conscious rate-limit change that updates this floor
// together with SECURITY.md.
const scopedLimiterFloor = 5

// scopeGuardBudget is the per-limiter budget the guard runs every limiter at:
// one request inside the budget, the next one over it. Small enough that the
// overflow is unambiguous, large enough that a limiter cannot refuse the first
// request.
const scopeGuardBudget = 1

// scopeGuardNeighbourSuffix builds a path that shares a scoped limiter's prefix
// without being it. Nothing registers this suffix; the point is only that the
// normalized comparison must still miss it.
const scopeGuardNeighbourSuffix = "/rate-limit-scope-probe"

// fiberMethodConstants is the complete set of verbs fiber exposes as constants,
// so a call site written with any of them resolves without a per-limiter entry.
func fiberMethodConstants() map[string]string {
	return map[string]string{
		"MethodGet":     fiber.MethodGet,
		"MethodHead":    fiber.MethodHead,
		"MethodPost":    fiber.MethodPost,
		"MethodPut":     fiber.MethodPut,
		"MethodPatch":   fiber.MethodPatch,
		"MethodDelete":  fiber.MethodDelete,
		"MethodConnect": fiber.MethodConnect,
		"MethodOptions": fiber.MethodOptions,
		"MethodTrace":   fiber.MethodTrace,
		"MethodQuery":   fiber.MethodQuery,
	}
}

// limiterPathConstants resolves the qualified constants a call site may spell
// its path as, exactly the way fiberMethodConstants resolves the verbs. A path
// that the limiter wiring, the route registration and the refusal
// classification all have to agree on is declared once as a constant on
// purpose — a second copy is how one of the three silently stops matching the
// other two — so the guard follows the constant rather than forcing the literal
// back. An unqualified or unknown constant still fails: this map is a lookup,
// not an escape hatch.
func limiterPathConstants() map[string]string {
	return map[string]string{
		"api.LanguageSwitchPath": api.LanguageSwitchPath,
	}
}

// discoverScopedLimiterSpecs parses every non-test file in the package and
// returns one spec per rateLimitOnlyFor call site.
func discoverScopedLimiterSpecs(t *testing.T) []scopedLimiterSpec {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fileSet := token.NewFileSet()
	var specs []scopedLimiterSpec
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fileSet, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, ok := call.Fun.(*ast.Ident)
			if !ok || callee.Name != "rateLimitOnlyFor" {
				return true
			}
			position := fileSet.Position(call.Pos()).String()
			if len(call.Args) != 2 {
				t.Fatalf("%s: rateLimitOnlyFor now takes %d argument(s); teach this guard the new shape rather than letting a limiter go unswept", position, len(call.Args))
			}
			specs = append(specs, scopedLimiterSpec{
				method: limiterMethodArgument(t, position, call.Args[0]),
				path:   limiterPathArgument(t, position, call.Args[1]),
				source: position,
			})
			return true
		})
	}

	if len(specs) < scopedLimiterFloor {
		t.Fatalf("found %d scoped rate limiter(s), expected at least %d — either discovery broke or a limiter was removed; both need a conscious review, not a green run", len(specs), scopedLimiterFloor)
	}
	return specs
}

func limiterMethodArgument(t *testing.T, position string, expr ast.Expr) string {
	t.Helper()

	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("%s: the method argument is not a fiber.Method* constant; this guard reads it statically, so spell it as one", position)
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "fiber" {
		t.Fatalf("%s: the method argument is not a fiber.Method* constant; this guard reads it statically, so spell it as one", position)
	}
	method, ok := fiberMethodConstants()[selector.Sel.Name]
	if !ok {
		t.Fatalf("%s: unknown method constant fiber.%s — add it to fiberMethodConstants so the limiter stays swept", position, selector.Sel.Name)
	}
	return method
}

func limiterPathArgument(t *testing.T, position string, expr ast.Expr) string {
	t.Helper()

	if selector, isSelector := expr.(*ast.SelectorExpr); isSelector {
		pkg, isIdent := selector.X.(*ast.Ident)
		if !isIdent {
			t.Fatalf("%s: the path argument is not a string literal or a known package constant; this guard reads it statically, so keep the path inline or teach the guard the constant", position)
		}
		qualified := pkg.Name + "." + selector.Sel.Name
		path, known := limiterPathConstants()[qualified]
		if !known {
			t.Fatalf("%s: unknown path constant %s — add it to limiterPathConstants so the limiter stays swept", position, qualified)
		}
		return path
	}

	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		t.Fatalf("%s: the path argument is not a string literal or a known package constant; this guard reads it statically, so keep the path inline or teach the guard the constant", position)
	}
	path, err := strconv.Unquote(literal.Value)
	if err != nil {
		t.Fatalf("%s: unquote path literal %s: %v", position, literal.Value, err)
	}
	return path
}

// routableSpellings returns the spellings of a path that fiber routes to the
// same handler under the app's configuration: CaseSensitive and StrictRouting
// are both off, so case and trailing slashes are folded away before matching.
// TestFiberRoutesEveryScopedLimiterPathSpellingToItsOwnRoute proves each of
// these really does reach the guarded route rather than a 404.
func routableSpellings(path string) []string {
	upper := strings.ToUpper(path)
	return []string{upper, path + "/", upper + "/", path + "//"}
}

// uniformRateLimits sets EVERY budget in rateLimitSettings by reflection rather
// than by naming the fields. A limiter added later brings a new pair of fields,
// and a hand-written literal would leave them zero — fiber's limiter silently
// substitutes its own defaults for a zero Max/Expiration, so the new limiter
// would run at 5/minute while the guard believed it ran at scopeGuardBudget.
func uniformRateLimits(t *testing.T, budget int, window time.Duration) rateLimitSettings {
	t.Helper()

	settings := rateLimitSettings{}
	value := reflect.ValueOf(&settings).Elem()
	durationType := reflect.TypeOf(time.Duration(0))
	for i := range value.NumField() {
		field := value.Field(i)
		switch {
		case field.Type() == durationType:
			field.SetInt(int64(window))
		case field.Kind() == reflect.Int:
			field.SetInt(int64(budget))
		default:
			t.Fatalf("rateLimitSettings.%s is a %s; this guard sets every budget by reflection and cannot leave one at fiber's default", value.Type().Field(i).Name, field.Type())
		}
	}
	return settings
}

// newScopeGuardApp builds the REAL middleware chain — fiber.New over the
// production fiberConfig, then configureFiberMiddleware — with every budget at
// scopeGuardBudget. The config matters as much as the middleware: on a bare
// fiber.New() the routing flags happen to carry the same defaults, but the
// guard is about what the shipped app routes, so it starts from what the shipped
// app is built with. A terminal catch-all stands in for the route table, which
// api.RegisterRoutes owns; every request that survives the chain ends there.
func newScopeGuardApp(t *testing.T, handler *api.Handler) *fiber.App {
	t.Helper()

	app := fiber.New(fiberConfig(proxySettings{}))
	configureFiberMiddleware(app, runtimeConfig{
		RateLimits: uniformRateLimits(t, scopeGuardBudget, time.Minute),
	}, handler)
	app.Use(func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	return app
}

// overflowRateLimitKey spends the budget on target and returns the stable error
// key of whichever limiter answered the request that went over it, or "" when
// nothing capped it. Each call gets its own app so the buckets start empty:
// under app.Test every request reports the same peer address, so one app would
// share one bucket across the spellings and hide which of them was counted.
func overflowRateLimitKey(t *testing.T, handler *api.Handler, method, target string) string {
	t.Helper()

	app := newScopeGuardApp(t, handler)
	if status, _ := scopeGuardSend(t, app, method, target); status == http.StatusTooManyRequests {
		t.Fatalf("%s %s was refused on the first request; the guard needs one request inside the budget of %d", method, target, scopeGuardBudget)
	}

	status, body := scopeGuardSend(t, app, method, target)
	if status != http.StatusTooManyRequests {
		return ""
	}
	payload := struct {
		Error string `json:"error"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode the 429 answering %s %s: %v (body %q)", method, target, err, body)
	}
	if payload.Error == "" {
		t.Fatalf("the 429 answering %s %s carries no stable error key (body %q)", method, target, body)
	}
	return payload.Error
}

// scopeGuardSend drives one request through the chain. It asks for JSON so the
// rate-limit answer is the envelope carrying the stable key rather than the
// HTML form redirect the auth limiters serve a browser.
func scopeGuardSend(t *testing.T, app *fiber.App, method, target string) (int, []byte) {
	t.Helper()

	request := httptest.NewRequest(method, target, strings.NewReader(""))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode, mustReadAll(t, response)
}

// TestScopedRateLimitersCoverEveryRoutableSpellingOfTheirPath is the sweep over
// the whole class: for every limiter scoped to a (method, path), each spelling
// fiber routes to that path must be capped by THAT limiter — same stable error
// key as the canonical spelling — and a path merely sharing its prefix must not
// be. Comparing the answering key rather than the bare 429 is what keeps the
// four /api limiters honest: on the raw-byte comparison their variants still hit
// the /api catch-all, so status alone reports a cap that is 300/min instead of
// the route's own 8/15min.
func TestScopedRateLimitersCoverEveryRoutableSpellingOfTheirPath(t *testing.T) {
	handler := newRateLimitTestHandler(t)

	for _, spec := range discoverScopedLimiterSpecs(t) {
		t.Run(spec.method+" "+spec.path, func(t *testing.T) {
			canonical := overflowRateLimitKey(t, handler, spec.method, spec.path)
			if canonical == "" {
				t.Fatalf("%s: %s %s is uncapped on its own canonical spelling — the limiter no longer covers the path it is scoped to", spec.source, spec.method, spec.path)
			}

			for _, variant := range routableSpellings(spec.path) {
				got := overflowRateLimitKey(t, handler, spec.method, variant)
				switch got {
				case canonical:
				case "":
					t.Errorf("%s: %s %s reaches the same handler as %s and NOTHING capped it — the scope predicate compares the raw path, so a change of case or a trailing slash spends no budget at all", spec.source, spec.method, variant, spec.path)
				default:
					t.Errorf("%s: %s %s was capped by %q instead of the route's own %q — the scope predicate missed the spelling and let it fall through to a broader limiter", spec.source, spec.method, variant, got, canonical)
				}
			}

			// Scope precision: matching on the routing normalization must not
			// widen the comparison into a prefix match. POST /api/v1/sessions
			// /2fa-challenge sharing the login budget is the failure this
			// predicate exists to prevent.
			neighbour := strings.TrimRight(spec.path, "/") + scopeGuardNeighbourSuffix
			if got := overflowRateLimitKey(t, handler, spec.method, neighbour); got == canonical {
				t.Errorf("%s: %s %s spends the budget of %s — the scope predicate has widened into a prefix match", spec.source, spec.method, neighbour, spec.path)
			}
		})
	}
}

// TestFiberRoutesEveryScopedLimiterPathSpellingToItsOwnRoute pins the framework
// seam the fix rests on, in both directions.
//
// Forward: each spelling in routableSpellings really is routed to the scoped
// path's own handler, so a limiter that skips it leaves a reachable route
// uncapped — the sweep above is measuring something real, not spellings that
// 404 anyway.
//
// Backward: c.Path() still hands back the untouched path off the wire while the
// router matches its own normalized copy. That gap is the whole defect; should a
// fiber upgrade close it by normalizing c.Path() too, this pin fails and says so
// rather than leaving routingNormalizedPath as dead weight nobody revisits.
func TestFiberRoutesEveryScopedLimiterPathSpellingToItsOwnRoute(t *testing.T) {
	specs := discoverScopedLimiterSpecs(t)

	app := fiber.New(fiberConfig(proxySettings{}))
	var observedPath string
	app.Use(func(c fiber.Ctx) error {
		observedPath = c.Path()
		return c.Next()
	})
	for _, spec := range specs {
		routePath := spec.path
		app.Add([]string{spec.method}, routePath, func(c fiber.Ctx) error {
			return c.SendString(routePath)
		})
	}

	for _, spec := range specs {
		t.Run(spec.method+" "+spec.path, func(t *testing.T) {
			for _, variant := range routableSpellings(spec.path) {
				observedPath = ""
				request := httptest.NewRequest(spec.method, variant, strings.NewReader(""))
				response, err := app.Test(request, testConfigNoTimeout)
				if err != nil {
					t.Fatalf("%s %s: %v", spec.method, variant, err)
				}
				body := mustReadAll(t, response)
				_ = response.Body.Close()

				if response.StatusCode != http.StatusOK || string(body) != spec.path {
					t.Fatalf("%s %s answered %d %q; it was expected to route to %s, which is why the limiter scoped to that path has to count it", spec.method, variant, response.StatusCode, body, spec.path)
				}
				if observedPath != variant {
					t.Fatalf("c.Path() returned %q for %s %s: fiber now hands the handler a normalized path, so routingNormalizedPath is no longer mirroring an unexported detail — revisit it", observedPath, spec.method, variant)
				}
			}
		})
	}
}
