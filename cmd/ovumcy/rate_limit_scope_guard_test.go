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
	"github.com/ovumcy/ovumcy-web/internal/db"
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

// parsePackageFiles parses every non-test file in the package. Both guards that
// read the wiring statically walk what it returns, so a limiter mounted from any
// function in any file of package main is swept on the same footing as the ones
// configureFiberMiddleware mounts.
func parsePackageFiles(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fileSet := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fileSet, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		files = append(files, parsed)
	}
	if len(files) == 0 {
		t.Fatal("no non-test file parsed in the package directory — discovery broke, and every guard reading the wiring would sweep nothing")
	}
	return fileSet, files
}

// discoverScopedLimiterSpecs returns one spec per rateLimitOnlyFor call site in
// the package.
func discoverScopedLimiterSpecs(t *testing.T) []scopedLimiterSpec {
	t.Helper()

	fileSet, files := parsePackageFiles(t)
	var specs []scopedLimiterSpec
	for _, parsed := range files {
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
// rather than leaving httpx.RoutingNormalizedPath as dead weight nobody revisits.
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
					t.Fatalf("c.Path() returned %q for %s %s: fiber now hands the handler a normalized path, so httpx.RoutingNormalizedPath is no longer mirroring an unexported detail — revisit it", observedPath, spec.method, variant)
				}
			}
		})
	}
}

// The calendar feed's limiter is the one scoped limiter discoverScopedLimiterSpecs
// cannot read: its path carries a per-owner token, so there is no exact
// (method, path) pair for rateLimitOnlyFor to compare. It carries its own Next
// instead — api.IsCalendarFeedRequest, the route-SHAPE predicate the CSRF skip and
// the language skip key on as well — and the three tests below are that mount's
// half of the sweep above: the spellings it must count, the neighbours under its
// prefix it must not, and a static check that no limiter here grows a third kind
// of scope predicate with nothing sweeping it.

// calendarFeedLimiterBudget is the feed budget the two behavioural tests below
// run at: one request inside it, the next one over it.
const calendarFeedLimiterBudget = 1

// calendarFeedSpellingPlaceholder is the feed URL with its token masked. The
// spellings below are derived from it by the same routableSpellings
// transformation that derives the real targets, so a subtest name or a failure
// message can name a spelling without printing a live subscribe token.
const calendarFeedSpellingPlaceholder = api.CalendarFeedRateLimitPrefix + "/<token>.ics"

// calendarFeedSend drives one request through app and returns its status.
// spelling is the masked form of target, used for diagnostics only.
func calendarFeedSend(t *testing.T, app *fiber.App, method, spelling, target string) int {
	t.Helper()

	response, err := app.Test(httptest.NewRequest(method, target, nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s %s: %v", method, spelling, err)
	}
	_ = response.Body.Close()
	return response.StatusCode
}

// TestCalendarFeedLimiterCountsEveryRoutableSpellingAndVerbOfTheFeedURL is the
// feed limiter's leg of TestScopedRateLimitersCoverEveryRoutableSpellingOfTheirPath:
// every spelling of the feed URL api.IsCalendarFeedRequest claims — case-folded,
// trailing slash alike — must spend this budget, on both verbs the predicate
// claims, because the limiter keys on that same predicate. A Next comparing the
// raw c.Path() against one canonical string would leave /CALENDAR/FEED/<token>.ICS
// and /calendar/feed/<token>.ics/ polling an unauthenticated surface uncapped
// while the sweep above stayed green, having never seen this mount at all.
//
// HEAD is counted because the predicate claims it, not because the deployed route
// table answers it from ServeCalendarFeed: fiber appends a GET route's
// auto-generated HEAD copy at serve time, after the terminal
// app.Use(handler.NotFound), so a HEAD to any page route in the shipped app lands
// in NotFound. That predates this change, is not feed-specific, and is pinned by
// TestIsCalendarFeedRequestMatchesWhatFiberActuallyDispatches — what matters here
// is that the two cookie skips act on a HEAD to this path, so the limiter reading
// the same predicate has to charge it.
func TestCalendarFeedLimiterCountsEveryRoutableSpellingAndVerbOfTheFeedURL(t *testing.T) {
	handler, database := newRateLimitTestHandlerAndDB(t)
	user := seedOwner(t, db.NewRepositories(database), "calendar-feed-spelling@example.com", 14)
	feedTarget := api.CalendarFeedRateLimitPrefix + "/" + armCalendarFeedToken(t, database, user.ID) + ".ics"

	targets := append([]string{feedTarget}, routableSpellings(feedTarget)...)
	spellings := append([]string{calendarFeedSpellingPlaceholder}, routableSpellings(calendarFeedSpellingPlaceholder)...)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for i, target := range targets {
			t.Run(method+" "+spellings[i], func(t *testing.T) {
				// A fresh app per case: under app.Test every request reports the
				// same peer address, so one app would share one bucket across the
				// spellings and hide which of them was counted.
				app := newCalendarFeedTestApp(t, handler, calendarFeedLimiterBudget)
				if status := calendarFeedSend(t, app, method, spellings[i], target); status == http.StatusTooManyRequests {
					t.Fatalf("%s %s was refused on the first request; this sweep needs one request inside the budget of %d", method, spellings[i], calendarFeedLimiterBudget)
				}
				if status := calendarFeedSend(t, app, method, spellings[i], target); status != http.StatusTooManyRequests {
					t.Fatalf("%s %s answered %d once the feed budget of %d was spent: api.IsCalendarFeedRequest claims this spelling for the feed, and the CSRF and language skips act on it, so the limiter reading that same predicate has to count it", method, spellings[i], status, calendarFeedLimiterBudget)
				}
			})
		}
	}
}

// TestCalendarFeedLimiterSpendsNoBudgetOnPathsThatReachNoFeed is the other half,
// and covers what TestCalendarFeedRateLimiterScopedToTheRouteShapeNotTheBarePrefix
// covered before it, one bare-prefix case of the three below.
// The budget is small because a well-formed token costs a keyed-MAC compare — or,
// on a not-yet-migrated row, a bcrypt — and a request that reaches no feed pays
// neither, which is why SECURITY.md's calendar-feed row says a bare prefix, a
// nested path segment and a non-GET/HEAD verb spend no part of it. app.Use is
// prefix-matched and method-agnostic, so those three are exactly what an unscoped
// mount would charge. Each runs on its own app, so a scope that widened onto one
// of them alone reddens that case alone; the two real polls closing each case are
// its positive anchor, the first proving the budget was still whole and the
// second that there was a budget at all.
func TestCalendarFeedLimiterSpendsNoBudgetOnPathsThatReachNoFeed(t *testing.T) {
	handler, database := newRateLimitTestHandlerAndDB(t)
	user := seedOwner(t, db.NewRepositories(database), "calendar-feed-uncounted@example.com", 14)
	feedTarget := api.CalendarFeedRateLimitPrefix + "/" + armCalendarFeedToken(t, database, user.ID) + ".ics"

	uncounted := []struct {
		name   string
		method string
		target string
	}{
		{"the bare prefix", http.MethodGet, api.CalendarFeedRateLimitPrefix + "/"},
		{"a nested path segment", http.MethodGet, api.CalendarFeedRateLimitPrefix + "/a/b.ics"},
		{"a non-GET or HEAD verb on the feed's own URL", http.MethodPost, feedTarget},
	}
	for _, probe := range uncounted {
		t.Run(probe.name, func(t *testing.T) {
			app := newCalendarFeedTestApp(t, handler, calendarFeedLimiterBudget)
			for attempt := range 2 {
				if status := calendarFeedSend(t, app, probe.method, probe.name, probe.target); status == http.StatusTooManyRequests {
					t.Fatalf("request %d to %s was refused by the feed's limiter — its scope has widened past the route this budget exists to bound", attempt+1, probe.name)
				}
			}

			if status := calendarFeedSend(t, app, http.MethodGet, calendarFeedSpellingPlaceholder, feedTarget); status != http.StatusOK {
				t.Fatalf("the first real poll answered %d after two requests to %s: they spent part of a budget of %d that only a request reaching the feed may spend", status, probe.name, calendarFeedLimiterBudget)
			}
			if status := calendarFeedSend(t, app, http.MethodGet, calendarFeedSpellingPlaceholder, feedTarget); status != http.StatusTooManyRequests {
				t.Fatalf("the second real poll answered %d: the feed budget of %d is not enforced on this app at all, so the two requests to %s prove nothing", status, calendarFeedLimiterBudget, probe.name)
			}
		})
	}
}

// The two Next predicates a limiter may carry, each covered by a sweep in this
// file: rateLimitOnlyFor by TestScopedRateLimitersCoverEveryRoutableSpellingOfTheirPath,
// which reads its arguments out of the source, and the feed's route-shape
// predicate by the two tests above, which drive it. A limiter with no Next at all
// is outside that half of the guard's subject — it is mounted prefix-wide on
// purpose (the /api and /auth/oidc catch-alls), and fiber normalizes a prefix
// mount itself. Its key generator is not: every limiter, scoped or prefix-wide,
// has to key per client.
const (
	scopedPathPredicateForm = "rateLimitOnlyFor(method, path)"
	routeShapePredicateForm = "api.IsCalendarFeedRequest(c.Method(), c.Path())"
)

// limiterConfigFloor guards against a vacuous pass, the way scopedLimiterFloor
// does for the call sites: the sweep below walks the package for limiter.Config
// literals, so a walk that silently matched none would report success over an
// empty set. Eight limiters are wired today — logout, login, register,
// forgot-password, the /auth/oidc catch-all, the language switch, the /api
// catch-all and the calendar feed.
const limiterConfigFloor = 8

// isQualified reports whether expr is the qualified name pkg.name.
func isQualified(expr ast.Expr, pkg, name string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	ident, isIdent := selector.X.(*ast.Ident)
	return isIdent && ident.Name == pkg
}

// constructsLimiterConfig reports whether expr builds a limiter.Config, with or
// without the address-of a mount could be written with.
func constructsLimiterConfig(expr ast.Expr) bool {
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unary.X
	}
	literal, ok := expr.(*ast.CompositeLit)
	return ok && isQualified(literal.Type, "limiter", "Config")
}

// limiterConfigVariables returns the names bound to a limiter.Config in file, so
// the sweep can refuse a config assembled field by field after the literal it
// reads has been closed.
func limiterConfigVariables(file *ast.File) map[string]bool {
	names := make(map[string]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.ValueSpec:
			if isQualified(typed.Type, "limiter", "Config") {
				for _, name := range typed.Names {
					names[name.Name] = true
				}
			}
			for i, value := range typed.Values {
				if i < len(typed.Names) && constructsLimiterConfig(value) {
					names[typed.Names[i].Name] = true
				}
			}
		case *ast.AssignStmt:
			for i, value := range typed.Rhs {
				if i >= len(typed.Lhs) || !constructsLimiterConfig(value) {
					continue
				}
				if ident, ok := typed.Lhs[i].(*ast.Ident); ok {
					names[ident.Name] = true
				}
			}
		}
		return true
	})
	return names
}

// keyGeneratorBindings returns the names bound to a rateLimitKeyGenerator(...)
// call in file. configureFiberMiddleware builds the generator once and hands the
// same value to every limiter, so the sweep has to follow that name to see what
// a limiter keys on.
func keyGeneratorBindings(file *ast.File) map[string]bool {
	names := make(map[string]bool)
	record := func(target ast.Expr, value ast.Expr) {
		call, ok := value.(*ast.CallExpr)
		if !ok {
			return
		}
		callee, isIdent := call.Fun.(*ast.Ident)
		if !isIdent || callee.Name != "rateLimitKeyGenerator" {
			return
		}
		if ident, isTarget := target.(*ast.Ident); isTarget {
			names[ident.Name] = true
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.ValueSpec:
			for i, value := range typed.Values {
				if i < len(typed.Names) {
					record(typed.Names[i], value)
				}
			}
		case *ast.AssignStmt:
			for i, value := range typed.Rhs {
				if i < len(typed.Lhs) {
					record(typed.Lhs[i], value)
				}
			}
		}
		return true
	})
	return names
}

// limiterConfigField returns the value a limiter.Config literal gives field, and
// whether the literal sets it at all.
func limiterConfigField(literal *ast.CompositeLit, field string) (ast.Expr, bool) {
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, isIdent := pair.Key.(*ast.Ident); isIdent && key.Name == field {
			return pair.Value, true
		}
	}
	return nil, false
}

// callsOn reports whether expr is the no-argument call receiver.method().
func callsOn(expr ast.Expr, receiver, method string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector || selector.Sel.Name != method {
		return false
	}
	ident, isIdent := selector.X.(*ast.Ident)
	return isIdent && ident.Name == receiver
}

// asksTheFeedPredicateAboutItsOwnRequest reports whether the func literal puts
// api.IsCalendarFeedRequest the question the two behavioural sweeps answer: the
// method and path of the request being scoped, taken off its own context, rather
// than a captured or constant pair. The shape is read from the syntax tree, so
// the same predicate laid out across several lines still matches.
func asksTheFeedPredicateAboutItsOwnRequest(literal *ast.FuncLit) bool {
	params := literal.Type.Params
	if params == nil || len(params.List) != 1 || len(params.List[0].Names) != 1 {
		return false
	}
	if !isQualified(params.List[0].Type, "fiber", "Ctx") {
		return false
	}
	context := params.List[0].Names[0].Name

	asked := false
	ast.Inspect(literal.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isQualified(call.Fun, "api", "IsCalendarFeedRequest") || len(call.Args) != 2 {
			return true
		}
		if callsOn(call.Args[0], context, "Method") && callsOn(call.Args[1], context, "Path") {
			asked = true
		}
		return !asked
	})
	return asked
}

// limiterScopePredicateForm names which of the two swept forms expr is, or ""
// for a third kind no sweep here drives.
func limiterScopePredicateForm(expr ast.Expr) string {
	if call, ok := expr.(*ast.CallExpr); ok {
		if callee, isIdent := call.Fun.(*ast.Ident); isIdent && callee.Name == "rateLimitOnlyFor" {
			return scopedPathPredicateForm
		}
		return ""
	}
	if literal, ok := expr.(*ast.FuncLit); ok && asksTheFeedPredicateAboutItsOwnRequest(literal) {
		return routeShapePredicateForm
	}
	return ""
}

// keysPerClient reports whether expr is the production key generator, either
// called inline or through a name bound to that call in the same file.
func keysPerClient(expr ast.Expr, bindings map[string]bool) bool {
	switch typed := expr.(type) {
	case *ast.CallExpr:
		callee, ok := typed.Fun.(*ast.Ident)
		return ok && callee.Name == "rateLimitKeyGenerator"
	case *ast.Ident:
		return bindings[typed.Name]
	}
	return false
}

// TestEveryLimiterScopePredicateIsOneTheSweepsCover closes the gap the feed's
// mount opened above: discovery there reads rateLimitOnlyFor call sites, so a
// limiter scoped by any other predicate is not swept — it is invisible, and
// rewriting the feed's Next to compare the raw c.Path() would leave every other
// guard in this file green.
//
// It reads the syntax tree of the whole package rather than the text of
// configureFiberMiddleware: a limiter mounted from newFiberApp — or from any
// function added later, in any file — ships in the same chain, and a text scan
// anchored on one function's name would not see it. Each limiter.Config must
// therefore carry a Next that is one of the forms a sweep here drives (or none
// at all, the deliberate prefix-wide mounts), a KeyGenerator that is the
// production per-client one, and both spelled inside the literal, where this
// sweep can read them. Each swept form must still be wired, or the sweep written
// for it is measuring a wiring that is gone.
//
// KeyGenerator is here because the two claims are one wiring: a limiter that
// pools every client into a single bucket caps the endpoint's total traffic, not
// each caller's, so the per-IP row in SECURITY.md would be describing something
// the chain no longer does. TestRateLimitKeyGeneratorBucketing proves that
// generator separates clients; this pins that every limiter uses it.
func TestEveryLimiterScopePredicateIsOneTheSweepsCover(t *testing.T) {
	fileSet, files := parsePackageFiles(t)

	wired := make(map[string]int, 2)
	limiters := 0
	for _, file := range files {
		configVariables := limiterConfigVariables(file)
		keyGenerators := keyGeneratorBindings(file)
		ast.Inspect(file, func(node ast.Node) bool {
			if assignment, ok := node.(*ast.AssignStmt); ok {
				for _, target := range assignment.Lhs {
					selector, isSelector := target.(*ast.SelectorExpr)
					if !isSelector || (selector.Sel.Name != "Next" && selector.Sel.Name != "KeyGenerator") {
						continue
					}
					if ident, isIdent := selector.X.(*ast.Ident); isIdent && configVariables[ident.Name] {
						t.Errorf("%s: %s.%s is set after the limiter.Config literal is closed; this sweep reads the literal, so a limiter configured this way is scoped and keyed by nothing it can see — keep Next and KeyGenerator inside the literal", fileSet.Position(target.Pos()), ident.Name, selector.Sel.Name)
					}
				}
				return true
			}

			literal, ok := node.(*ast.CompositeLit)
			if !ok || !isQualified(literal.Type, "limiter", "Config") {
				return true
			}
			limiters++
			position := fileSet.Position(literal.Pos()).String()

			if next, scoped := limiterConfigField(literal, "Next"); scoped {
				form := limiterScopePredicateForm(next)
				if form == "" {
					t.Errorf("%s: this limiter is scoped by a predicate nothing here sweeps: express the scope as rateLimitOnlyFor(method, path) so TestScopedRateLimitersCoverEveryRoutableSpellingOfTheirPath reads it, or — for a scope no exact (method, path) pair can express — write the sweep that drives every routable spelling of it first, then teach limiterScopePredicateForm the shape it took", position)
				} else {
					wired[form]++
				}
			}

			keyGenerator, keyed := limiterConfigField(literal, "KeyGenerator")
			if !keyed || !keysPerClient(keyGenerator, keyGenerators) {
				t.Errorf("%s: this limiter does not key on rateLimitKeyGenerator, so its budget is not per client: one bucket for every caller turns the cap into a total-traffic limit any single IP can spend on everyone else's behalf. Key it on rateLimitKeyGenerator(config.Proxy), which resolves the real client IP across a trusted proxy", position)
			}
			return true
		})
	}

	if limiters < limiterConfigFloor {
		t.Fatalf("found %d limiter(s), expected at least %d — either the walk broke or a limiter was removed; both need a conscious review, not a green run", limiters, limiterConfigFloor)
	}
	for _, form := range []string{scopedPathPredicateForm, routeShapePredicateForm} {
		if wired[form] == 0 {
			t.Errorf("no limiter is scoped by %s any more; the sweep written for it now measures a wiring that is gone", form)
		}
	}
}
