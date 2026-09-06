package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestOpenAPIContractMatchesRegisteredRoutes is the route↔spec contract guard:
// every registered /api/v1 route must be documented in docs/openapi.yaml and
// vice versa. It fails on drift in either direction — a new handler that the
// spec forgets, or a spec entry for a route that no longer exists — so the
// OpenAPI document cannot silently fall out of sync with the code.
//
// It is deliberately dependency-free: the spec's paths section is parsed
// line-by-line rather than pulling in a YAML library, matching the repo's
// minimal-dependency posture. Only the JSON-emitting /api/v1 surface is in
// scope; page routes are explicitly excluded from the contract (see the spec's
// own preamble).
func TestOpenAPIContractMatchesRegisteredRoutes(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	codeRoutes := registeredV1Routes(app)
	specRoutes := openAPIV1Routes(t, filepath.Join("..", "..", "docs", "openapi.yaml"))

	if len(codeRoutes) == 0 {
		t.Fatal("no /api/v1 routes discovered from the app; test setup is wrong")
	}
	// Positive anchor for the transport-twin filter registeredV1Routes applies:
	// a HEAD route with a chain of its own is an operation and must survive it.
	// Were the filter to start swallowing every HEAD route, both comparisons
	// below would pass while the spec's own HEAD entry went unchecked.
	if _, ok := codeRoutes[fiber.MethodHead+" /api/v1/days/{date}"]; !ok {
		t.Fatal("HEAD /api/v1/days/{date} is missing from the discovered routes: the transport-twin filter is dropping HEAD routes that carry their own handler chain")
	}
	if len(specRoutes) == 0 {
		t.Fatal("no /api/v1 paths parsed from openapi.yaml; parser or spec is wrong")
	}

	if missing := difference(codeRoutes, specRoutes); len(missing) > 0 {
		t.Errorf("routes registered in code but missing from docs/openapi.yaml:\n  %s", strings.Join(missing, "\n  "))
	}
	if extra := difference(specRoutes, codeRoutes); len(extra) > 0 {
		t.Errorf("routes documented in docs/openapi.yaml but not registered in code:\n  %s", strings.Join(extra, "\n  "))
	}
}

// TestOpenAPIDeclaresOnlyStatusesTheServerCanEmit is the status half of the
// route↔spec contract. Route presence is pinned above; this pins the response
// codes each operation publishes, because a spec can name every route correctly
// and still describe outcomes the server has no branch for. It did: the document
// declared '422' on nineteen operations while the app answers every validation
// refusal with 400 — the distinction clients actually get is
// error_detail.category, not a second status.
//
// The check runs one way on purpose: every status the spec DECLARES must be a
// status the server can PRODUCE. The opposite direction is not derivable from a
// source scan — a status appears in the sources without saying which operation
// answers it, and the document deliberately excludes page routes (the OIDC start
// redirect's 307) and documents the transport statuses centrally rather than per
// path. That half is pinned instead by
// TestOpenAPIDocumentsEveryTransportStatusTheEnvelopeCovers, where a real
// registry exists to enumerate.
//
// "Can produce" is read from the server's own sources: every rejection resolves
// its status through an APIErrorSpec built with a fiber.Status* constant, and the
// handful of direct answers use c.Status/SendStatus. Test sources are excluded —
// what a test can assert is not what the server can send.
func TestOpenAPIDeclaresOnlyStatusesTheServerCanEmit(t *testing.T) {
	repoRoot := filepath.Join("..", "..")

	declared := openAPIDeclaredStatuses(t, filepath.Join(repoRoot, "docs", "openapi.yaml"))
	if len(declared) == 0 {
		t.Fatal("no response statuses parsed from openapi.yaml; parser or spec is wrong")
	}
	sources := serverSourceText(t, repoRoot)

	statuses := make([]int, 0, len(declared))
	for status := range declared {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)

	for _, status := range statuses {
		identifier := fiberStatusIdentifier(t, status)
		if regexp.MustCompile(regexp.QuoteMeta(identifier) + `\b`).MatchString(sources) {
			continue
		}
		if regexp.MustCompile(`(?:Status|SendStatus)\(\s*` + fmt.Sprint(status) + `\b`).MatchString(sources) {
			continue
		}
		operations := declared[status]
		sort.Strings(operations)
		t.Errorf("docs/openapi.yaml declares %d but no server source emits it (searched for %s and a literal Status(%d)); declared on:\n  %s",
			status, identifier, status, strings.Join(operations, "\n  "))
	}
}

// apiRateLimitMountPattern locates the app-wide "/api" limiter's own
// limiter.New(limiter.Config{...}) block in cmd/ovumcy/server.go — the exact
// mount TestOpenAPIDeclaresRateLimitedOnEveryLimiterCoveredOperation's
// "every registered /api/v1 route is limiter-covered" premise depends on. It
// is anchored on the literal `app.Use("/api", limiter.New(limiter.Config{`,
// which is unique to this mount: the per-account limiters above it in the
// same file scope themselves with a `Next` filter instead of a path prefix,
// and the calendar-feed limiter mounts on a different prefix entirely.
var apiRateLimitMountPattern = regexp.MustCompile(`(?s)app\.Use\("/api", limiter\.New\(limiter\.Config\{(.*?)\n\t\}\)\)`)

// requireAPIRateLimitMountHasNoNextFilter pins the premise the sweep below
// rests on, rather than assuming it: registeredV1Routes stands in for "every
// limiter-covered /api/v1 route" only because the app-wide limiter mount
// carries no `Next` filter and therefore excludes nothing. The test app this
// file assembles (newOnboardingTestApp) never mounts cmd/ovumcy's real
// middleware chain — that wiring lives in cmd/ovumcy's newFiberApp, in
// package main, which internal/api cannot import (cmd depends on internal/api,
// never the reverse) — so the premise is checked by reading the mount's own
// source text instead of executing it, the same way
// TestOpenAPIDeclaresOnlyStatusesTheServerCanEmit reads server sources rather
// than running the server.
//
// If this ever fails, the sweep below is no longer sound: a route the mount
// newly exempts (a `Next` filter added to this exact block) can no longer
// answer 429, docs/openapi.yaml would keep declaring it anyway, and nothing
// else in this file would notice — the same "N of N+1" shape this whole test
// exists to close, one level up in the guard itself.
func requireAPIRateLimitMountHasNoNextFilter(t *testing.T) {
	t.Helper()
	path := filepath.Join("..", "..", "cmd", "ovumcy", "server.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	match := apiRateLimitMountPattern.FindSubmatch(data)
	if match == nil {
		t.Fatalf(`%s: no app.Use("/api", limiter.New(limiter.Config{...})) mount found — `+
			`the app-wide API limiter moved or was rewritten; update apiRateLimitMountPattern `+
			`before trusting registeredV1Routes as the limiter-covered set again`, path)
	}
	if strings.Contains(string(match[1]), "Next:") {
		t.Fatalf(`%s: the app-wide "/api" limiter now carries a Next filter, so it no longer `+
			`covers every /api/v1 route unconditionally — TestOpenAPIDeclaresRateLimitedOnEveryLimiterCoveredOperation's `+
			`"every registered route is limiter-covered" premise no longer holds. Derive the covered `+
			`set from the exemption (exclude the routes the filter skips) instead of the full route `+
			`table before trusting this sweep again`, path)
	}
}

// TestOpenAPIDeclaresRateLimitedOnEveryLimiterCoveredOperation pins the class
// behind API-3: the app-wide API limiter (cmd/ovumcy/server.go's
// `app.Use("/api", limiter.New(...))`, mounted before api.RegisterRoutes with
// no `Next` filter) sits in front of every `/api/v1/*` route, not only the
// handful that also carry a narrower per-account budget — so 429 is a real
// answer on every operation this spec documents. The document previously
// declared RateLimited on 12 of 47 operations, which is exactly the "N of
// N+1" partial fix this test exists to close: a prior change added 429 to the
// per-account-reauth-budget endpoints and stopped there, leaving every other
// operation's 429 real but undocumented.
//
// The set to check is read from the registered routes themselves — the same
// registeredV1Routes the route-presence contract above already trusts as the
// ground truth for "what /api/v1 operations exist" — rather than hand-listed,
// so a new route added later inherits the check with no edit here. That
// stand-in is sound only while requireAPIRateLimitMountHasNoNextFilter holds;
// it is asserted first so a mount that grows an exemption reddens this test
// instead of leaving the spec free to over-declare 429 with nothing to catch it.
func TestOpenAPIDeclaresRateLimitedOnEveryLimiterCoveredOperation(t *testing.T) {
	requireAPIRateLimitMountHasNoNextFilter(t)

	app, _ := newOnboardingTestApp(t)

	limiterCovered := registeredV1Routes(app)
	if len(limiterCovered) == 0 {
		t.Fatal("no /api/v1 routes discovered from the app; test setup is wrong")
	}

	declared := openAPIDeclaredStatuses(t, filepath.Join("..", "..", "docs", "openapi.yaml"))
	declaresRateLimited := make(map[string]struct{}, len(declared[http.StatusTooManyRequests]))
	for _, operation := range declared[http.StatusTooManyRequests] {
		declaresRateLimited[operation] = struct{}{}
	}

	var missing []string
	for operation := range limiterCovered {
		if _, ok := declaresRateLimited[operation]; !ok {
			missing = append(missing, operation)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("the app-wide /api limiter (cmd/ovumcy/server.go, mounted with no Next filter) can answer 429 on every /api/v1 operation, but docs/openapi.yaml declares no '429': RateLimited response for:\n  %s",
			strings.Join(missing, "\n  "))
	}
}

// TestOpenAPIDocumentsEveryTransportStatusTheEnvelopeCovers pins the reverse
// direction for the one part of the surface that keeps a registry of it. The
// transport statuses are answered outside any operation — an unroutable request,
// an undecodable body, an unparseable head — so the spec documents them once in
// the ApiError schema description instead of per path. That list is prose, which
// is exactly the kind of text that stops matching the map beside it: a new entry
// in transportErrorSpecsByStatus, or a renamed key, is invisible until a client
// meets a status the document never mentions. Both the status and its stable key
// are asserted, since the key is the whole reason the entry is worth publishing.
func TestOpenAPIDocumentsEveryTransportStatusTheEnvelopeCovers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	spec := string(data)

	statuses := make([]int, 0, len(transportErrorSpecsByStatus))
	for status := range transportErrorSpecsByStatus {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)

	for _, status := range statuses {
		entry := fmt.Sprintf("* `%d` — `%s`", status, transportErrorSpecsByStatus[status].Key)
		if !strings.Contains(spec, entry) {
			t.Errorf("transportErrorSpecsByStatus answers %d with key %q, but docs/openapi.yaml never documents it; the ApiError description needs the line %q",
				status, transportErrorSpecsByStatus[status].Key, entry)
		}
	}
}

// TestOpenAPIPublishesEveryErrorCategoryTheServerCanEmit pins the third half of
// the envelope contract. The status half is above; this one covers
// error_detail.category, which the spec publishes as a closed enum and which the
// spec's own text tells clients to branch on in preference to the key string. A
// closed enum missing a value the server sends is worse than a status the server
// never sends: the client is not merely waiting for something that cannot
// arrive, it is meeting something it was told could not exist. The enum omitted
// too_large, which every 413 refused by the body limit and every 431 refused by
// the read buffer carries.
//
// The check runs both ways, like the route contract: a constant the enum forgets
// AND an enum value no constant defines both fail. The constants are read out of
// their declaration file rather than listed here, so a category added later is
// swept without editing this test — and the parser is anchored to the compiler
// below, so a regex that silently stops matching cannot pass as "no categories
// to check".
func TestOpenAPIPublishesEveryErrorCategoryTheServerCanEmit(t *testing.T) {
	declared := declaredErrorCategories(t, filepath.Join("..", "..", "internal", "api", "error_mapping_types.go"))
	published := openAPIPublishedErrorCategories(t, filepath.Join("..", "..", "docs", "openapi.yaml"))

	// Anchor the source scan against the compiler: these two constants exist by
	// name here, so a regex that matched nothing (or matched the wrong capture)
	// fails loudly instead of reporting an empty, trivially satisfied set.
	for _, anchor := range []APIErrorCategory{APIErrorCategoryValidation, APIErrorCategoryTooLarge} {
		if _, ok := declared[string(anchor)]; !ok {
			t.Fatalf("category scan did not find %q in error_mapping_types.go; the scan, not the spec, is broken", anchor)
		}
	}

	if missing := difference(declared, published); len(missing) > 0 {
		t.Errorf("categories the server can emit but docs/openapi.yaml does not publish in ApiErrorDetail.category:\n  %s\n(a client told the enum is closed meets a value it was promised could not exist)",
			strings.Join(missing, "\n  "))
	}
	if extra := difference(published, declared); len(extra) > 0 {
		t.Errorf("categories published in docs/openapi.yaml but declared by no APIErrorCategory constant:\n  %s",
			strings.Join(extra, "\n  "))
	}
}

// declaredErrorCategories reads the APIErrorCategory constant block and returns
// the wire values it declares. Go cannot enumerate the members of a constant
// type at run time, so the declaration file is the only place that holds the
// whole set; scanning it keeps the sweep allowlist-free, which a hand-written
// slice here would not be.
func declaredErrorCategories(t *testing.T, sourcePath string) map[string]struct{} {
	t.Helper()
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read %s: %v", sourcePath, err)
	}

	declaration := regexp.MustCompile(`APIErrorCategory\w+\s+APIErrorCategory\s*=\s*"([^"]+)"`)
	categories := make(map[string]struct{})
	for _, match := range declaration.FindAllStringSubmatch(string(data), -1) {
		categories[match[1]] = struct{}{}
	}
	return categories
}

// openAPIPublishedErrorCategories reads the enum declared for
// ApiErrorDetail.category. The scan tracks the schema nesting rather than
// matching the first "enum:" it sees, because the sibling `target` property
// declares one too and confusing the two would compare the wrong sets.
func openAPIPublishedErrorCategories(t *testing.T, specPath string) map[string]struct{} {
	t.Helper()
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}

	inSchema, inCategory := false, false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))

		switch {
		case indent == 4 && strings.HasSuffix(text, ":"):
			inSchema = text == "ApiErrorDetail:"
			inCategory = false
		case indent == 8 && inSchema && strings.HasSuffix(text, ":"):
			inCategory = text == "category:"
		case indent == 10 && inCategory && strings.HasPrefix(text, "enum:"):
			values := make(map[string]struct{})
			for _, value := range strings.Split(strings.Trim(strings.TrimSpace(strings.TrimPrefix(text, "enum:")), "[]"), ",") {
				if trimmed := strings.Trim(strings.TrimSpace(value), `"'`); trimmed != "" {
					values[trimmed] = struct{}{}
				}
			}
			return values
		}
	}
	t.Fatal("no enum found for ApiErrorDetail.category in docs/openapi.yaml; parser or spec is wrong")
	return nil
}

// openAPIDeclaredStatuses returns every response status declared under paths:,
// mapped to the "METHOD /path" operations that declare it. Statuses live at a
// fixed depth — path item (2), operation (4), responses (6), status (8) — so the
// scan tracks that nesting rather than matching three-digit keys anywhere, which
// would also swallow enum values and example payloads.
func openAPIDeclaredStatuses(t *testing.T, specPath string) map[int][]string {
	t.Helper()
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}

	statusKey := regexp.MustCompile(`^'?(\d{3})'?:`)
	declared := make(map[int][]string)
	inPaths := false
	currentPath := ""
	currentOperation := ""
	inResponses := false

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			inPaths = strings.HasPrefix(line, "paths:")
			currentPath, currentOperation, inResponses = "", "", false
			continue
		}
		if !inPaths {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))
		text := strings.TrimSpace(line)

		switch {
		case indent == 2 && strings.HasPrefix(text, "/") && strings.HasSuffix(text, ":"):
			currentPath = strings.TrimSuffix(text, ":")
			currentOperation, inResponses = "", false
		case indent == 4:
			currentOperation = strings.ToUpper(strings.TrimSuffix(text, ":"))
			inResponses = false
		case indent == 6:
			inResponses = text == "responses:"
		case indent == 8 && inResponses && currentPath != "" && currentOperation != "":
			match := statusKey.FindStringSubmatch(text)
			if match == nil {
				continue
			}
			status := 0
			if _, err := fmt.Sscanf(match[1], "%d", &status); err != nil {
				t.Fatalf("unparseable status key %q under %s %s", text, currentOperation, currentPath)
			}
			declared[status] = append(declared[status], currentOperation+" "+currentPath)
		}
	}
	return declared
}

// serverSourceText concatenates every non-test Go source under internal/ and
// cmd/ — the code that can actually put a status on the wire.
func serverSourceText(t *testing.T, repoRoot string) string {
	t.Helper()
	var builder strings.Builder
	for _, tree := range []string{"internal", "cmd"} {
		root := filepath.Join(repoRoot, tree)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			builder.Write(data)
			builder.WriteString("\n")
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if builder.Len() == 0 {
		t.Fatalf("no server sources read from %s; test setup is wrong", repoRoot)
	}
	return builder.String()
}

// fiberStatusIdentifier derives the fiber constant a status would be written as.
// fiber's Status* constants mirror net/http's, whose names are StatusText with
// the separators removed, so the mapping needs no hand-maintained table that
// could drift from the spec it is meant to check.
func fiberStatusIdentifier(t *testing.T, status int) string {
	t.Helper()
	text := http.StatusText(status)
	if text == "" {
		t.Fatalf("docs/openapi.yaml declares %d, which is not a registered HTTP status", status)
	}
	return "fiber.Status" + strings.NewReplacer(" ", "", "-", "").Replace(text)
}

// registeredV1Routes returns the set of "METHOD /api/v1/..." entries the Fiber
// app has registered, with path params normalized to OpenAPI's {name} style.
func registeredV1Routes(app *fiber.App) map[string]struct{} {
	valid := map[string]bool{
		fiber.MethodGet:    true,
		fiber.MethodPost:   true,
		fiber.MethodPut:    true,
		fiber.MethodPatch:  true,
		fiber.MethodDelete: true,
		fiber.MethodHead:   true,
	}
	routes := make(map[string]struct{})
	// filterUseOption=true drops middleware/Use routes (e.g. group-level
	// AuthRequired/OwnerOnly), which otherwise surface as every method on a group
	// prefix and are not real endpoints.
	for _, route := range app.GetRoutes(true) {
		if !valid[route.Method] {
			continue
		}
		if !strings.HasPrefix(route.Path, "/api/v1") {
			continue
		}
		if isTransportHEADTwin(app, route) {
			continue
		}
		routes[route.Method+" "+fiberPathToOpenAPI(route.Path)] = struct{}{}
	}
	return routes
}

// isTransportHEADTwin reports whether route is the HEAD counterpart
// registerHEADTwins gives a GET route: the same handler chain, function for
// function. Such a route is not an operation of its own — it answers exactly
// what the GET operation answers, minus the body the protocol strips — so the
// spec states the rule once in its preamble instead of publishing a second copy
// of every GET operation, the same "one canonical route per operation" posture
// that kept the query-string day twin out.
//
// A HEAD route carrying a chain of its own is a real operation and stays in
// scope: HEAD /api/v1/days/{date} asks whether the day holds any data, which is
// not what GET on that path answers, and it is documented per path.
func isTransportHEADTwin(app *fiber.App, route fiber.Route) bool {
	if route.Method != fiber.MethodHead {
		return false
	}
	for _, candidate := range app.GetRoutes(true) {
		if candidate.Method != fiber.MethodGet || candidate.Path != route.Path {
			continue
		}
		return sameHandlerChain(candidate, route)
	}
	return false
}

// sameHandlerChain compares two routes by the functions fiber will dispatch
// rather than by their names: a twin is registered with the very handler values
// of the GET route it mirrors, so pointer identity is the exact question.
func sameHandlerChain(left fiber.Route, right fiber.Route) bool {
	if len(left.Handlers) == 0 || len(left.Handlers) != len(right.Handlers) {
		return false
	}
	for index := range left.Handlers {
		if reflect.ValueOf(left.Handlers[index]).Pointer() != reflect.ValueOf(right.Handlers[index]).Pointer() {
			return false
		}
	}
	return true
}

// fiberPathToOpenAPI rewrites Fiber ":param" segments to OpenAPI "{param}".
func fiberPathToOpenAPI(path string) string {
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		if strings.HasPrefix(segment, ":") {
			segments[index] = "{" + strings.TrimPrefix(segment, ":") + "}"
		}
	}
	return strings.Join(segments, "/")
}

// openAPIV1Routes extracts the set of "METHOD /api/v1/..." entries documented in
// the spec by scanning the paths section: 2-space-indented "/...:" keys are path
// items, 4-space-indented HTTP-method keys under them are operations. Only the
// /api/v1 prefix is kept.
func openAPIV1Routes(t *testing.T, specPath string) map[string]struct{} {
	t.Helper()
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}

	methods := map[string]string{
		"get": fiber.MethodGet, "post": fiber.MethodPost, "put": fiber.MethodPut,
		"patch": fiber.MethodPatch, "delete": fiber.MethodDelete, "head": fiber.MethodHead,
	}

	routes := make(map[string]struct{})
	inPaths := false
	currentPath := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		// A column-0 key starts a new top-level section; only "paths:" holds routes.
		if !strings.HasPrefix(line, " ") {
			inPaths = strings.HasPrefix(line, "paths:")
			currentPath = ""
			continue
		}
		if !inPaths {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))
		text := strings.TrimSpace(line)

		// Path item: exactly 2-space indent, "/....:".
		if indent == 2 && strings.HasPrefix(text, "/") && strings.HasSuffix(text, ":") {
			currentPath = strings.TrimSuffix(text, ":")
			continue
		}
		// Operation: exactly 4-space indent, an HTTP-method key.
		if indent == 4 && currentPath != "" {
			name := strings.TrimSuffix(text, ":")
			if method, ok := methods[name]; ok && strings.HasPrefix(currentPath, "/api/v1") {
				routes[method+" "+currentPath] = struct{}{}
			}
		}
	}
	return routes
}

// difference returns the sorted keys present in a but not in b.
func difference(a, b map[string]struct{}) []string {
	var only []string
	for key := range a {
		if _, ok := b[key]; !ok {
			only = append(only, key)
		}
	}
	sort.Strings(only)
	return only
}

// openAPIBoundKeywords are the numeric validation keywords this file reads out
// of the spec. Values are plain integers in every declaration, so a failed
// Atoi means the line is not one of ours and is skipped.
var openAPIBoundKeywords = map[string]struct{}{
	"minimum": {}, "maximum": {}, "minLength": {}, "maxLength": {},
}

// openAPISchemaPropertyBounds extracts the numeric validation keywords declared
// under components.schemas.<Schema>.properties.<property>, keyed
// "<Schema>.<property>". It extends openAPIV1Routes' indentation scan from the
// paths section to the components section rather than pulling in a YAML
// library, matching this file's dependency-free convention: schema names sit at
// 4-space indent, "properties:" at 6, property names at 8, and their keywords
// at 10. A property written as an inline map (`id: { type: integer }`) carries
// no bounds and is skipped; if one ever needs a bound, the sweep below fails
// loudly with "declares no <keyword>" rather than passing silently.
func openAPISchemaPropertyBounds(t *testing.T, specPath string) map[string]map[string]int {
	t.Helper()
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}

	bounds := make(map[string]map[string]int)
	inComponents, inSchemas, inProperties := false, false, false
	schema, property := "", ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		if !strings.HasPrefix(line, " ") {
			inComponents = strings.HasPrefix(line, "components:")
			inSchemas, inProperties = false, false
			schema, property = "", ""
			continue
		}
		if !inComponents {
			continue
		}

		switch indent := len(line) - len(strings.TrimLeft(line, " ")); {
		case indent == 2:
			// components holds parameters/responses/securitySchemes too;
			// only the schemas subtree carries property bounds.
			inSchemas = text == "schemas:"
			inProperties = false
			schema, property = "", ""
		case indent == 4 && inSchemas && strings.HasSuffix(text, ":"):
			schema = strings.TrimSuffix(text, ":")
			inProperties = false
			property = ""
		case indent == 6 && schema != "":
			inProperties = text == "properties:"
			property = ""
		case indent == 8 && inProperties && strings.HasSuffix(text, ":"):
			property = strings.TrimSuffix(text, ":")
		case indent == 10 && property != "":
			keyword, value, found := strings.Cut(text, ":")
			if !found {
				continue
			}
			if _, ok := openAPIBoundKeywords[keyword]; !ok {
				continue
			}
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				continue
			}
			key := schema + "." + property
			if bounds[key] == nil {
				bounds[key] = make(map[string]int)
			}
			bounds[key][keyword] = parsed
		}
	}
	return bounds
}

// TestOpenAPIRequiredRegistrationFieldsAreEnoughToRegister is the request-BODY
// half of the route↔spec contract, and it exists because the document described a
// registration no generated client could perform. `RegisterRequest` declared
// required [email, password, confirm_password] together with
// `additionalProperties: false`, while Register refuses every body whose
// `consent` is not truthy — so a client built from the spec was refused for
// omitting a field the same document forbade it to send. Measured on the soak
// stand, where the harness could not register until it added `consent`.
//
// Route presence, declared bounds, statuses and error categories were each
// pinned; required request fields were not, which is the same gap that let the
// 2FA challenge declare `application/json` for a handler reading form values.
//
// The area rule's shape: the constraint is READ from the spec and the endpoint
// judges it. The body carries exactly the declared required properties and
// nothing else, so a field the handler starts demanding without documenting it
// fails here — and so does a required field the spec gains that this test has no
// sample for, which is precisely when someone has to decide what a client would
// actually send.
func TestOpenAPIRequiredRegistrationFieldsAreEnoughToRegister(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	specPath := filepath.Join("..", "..", "docs", "openapi.yaml")
	required := openAPISchemaRequired(t, specPath, "RegisterRequest")
	if len(required) == 0 {
		t.Fatal("RegisterRequest declares no required properties in docs/openapi.yaml; parser or spec is wrong")
	}

	// What a client generated from the spec would put in each documented field:
	// values that satisfy every OTHER published constraint, so the only thing
	// this test can fail on is the completeness of the required set itself.
	samples := map[string]string{
		"email":            "spec-required-fields@example.com",
		"password":         "StrongPass1",
		"confirm_password": "StrongPass1",
		"consent":          "true",
	}
	body := make(map[string]string, len(required))
	for _, field := range required {
		value, ok := samples[field]
		if !ok {
			t.Fatalf("docs/openapi.yaml requires %q on RegisterRequest and this test carries no value for it — "+
				"add one a client could plausibly send, then let the endpoint judge it", field)
		}
		body[field] = value
	}

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal register body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response := mustAppResponse(t, app, request)
	if response.StatusCode == http.StatusBadRequest {
		envelope := map[string]any{}
		_ = json.NewDecoder(response.Body).Decode(&envelope)
		key := ""
		if detail, ok := envelope["error_detail"].(map[string]any); ok {
			key, _ = detail["key"].(string)
		}
		t.Fatalf("a body carrying exactly the spec's required fields %v was refused with 400 %q — "+
			"the document describes a registration no client written to it can perform", required, key)
	}
}

// openAPISchemaRequired reads one component schema's `required` list, which the
// spec writes inline (`required: [a, b, c]`). Line-based like every other reader
// here, for the same reason: the repo's minimal-dependency posture, and a YAML
// library would be pulled in for four lines of parsing.
func openAPISchemaRequired(t *testing.T, specPath, schema string) []string {
	t.Helper()
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}

	inComponents, inSchemas, inSchema := false, false, false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			inComponents = strings.HasPrefix(line, "components:")
			inSchemas, inSchema = false, false
			continue
		}
		if !inComponents {
			continue
		}
		switch indent := len(line) - len(strings.TrimLeft(line, " ")); {
		case indent == 2:
			inSchemas = text == "schemas:"
			inSchema = false
		case indent == 4 && inSchemas && strings.HasSuffix(text, ":"):
			inSchema = strings.TrimSuffix(text, ":") == schema
		case indent == 6 && inSchema:
			list, found := strings.CutPrefix(text, "required:")
			if !found {
				continue
			}
			names := []string{}
			for _, name := range strings.Split(strings.Trim(strings.TrimSpace(list), "[]"), ",") {
				if trimmed := strings.TrimSpace(name); trimmed != "" {
					names = append(names, trimmed)
				}
			}
			return names
		}
	}
	return nil
}

// openAPIDeclaredBound is one (schema, property, keyword) triple whose declared
// value must sit exactly where the running server draws its line. The bound is
// never restated here: the spec supplies the number and the endpoint judges it,
// so neither side can drift without the sweep going red.
type openAPIDeclaredBound struct {
	schema   string
	property string
	keyword  string
	method   string
	path     string
	okStatus int
	// body builds a request payload that is valid in every respect except
	// the field under probe, which carries `value` — an integer field's
	// value directly, a string field's length in runes.
	body func(value int) map[string]any
}

// openAPIBoundProbeRune is deliberately multi-byte: a length bound the server
// measured in bytes rather than runes would refuse a value the spec's
// character-counted maxLength permits, and the in-bounds probe would catch it.
const openAPIBoundProbeRune = "ж"

func openAPIBoundProbeText(runes int) string {
	return strings.Repeat(openAPIBoundProbeRune, runes)
}

// openAPIDeclaredBounds enumerates the request-shape bounds the /api/v1 surface
// enforces per field. Each entry names the endpoint that owns the check, so the
// oracle is the shipped request contract itself rather than a service-internal
// constant — several of these limits live in package-private policy functions
// that no exported symbol reveals.
//
// Decision (2026-07-28): OnboardingStep2Request stays out of this sweep even
// though it declares bounds on identically-named fields. POST
// /api/v1/onboarding/steps/2 CLAMPS an out-of-range cycle or period length into
// the accepted window (SanitizeOnboardingCycleAndPeriod) instead of refusing
// it, the way ReminderSettings.reminder_lead_days does, so the "one past the
// bound must be refused" probe below does not describe that endpoint at all —
// it would fail against correct behavior. Sharing one schema between the two is
// wrong for the same reason: they can carry the same numbers and still disagree
// on what happens when you exceed them. Any later attempt to cover the clamping
// endpoints needs its own oracle — assert the stored value came back inside the
// window — not an entry here.
var openAPIDeclaredBounds = []openAPIDeclaredBound{
	{
		schema: "ProfileSettings", property: "display_name", keyword: "maxLength",
		method: http.MethodPatch, path: "/api/v1/users/current/profile", okStatus: http.StatusOK,
		body: func(value int) map[string]any {
			return map[string]any{"display_name": openAPIBoundProbeText(value)}
		},
	},
	{
		schema: "CycleSettings", property: "cycle_length", keyword: "minimum",
		method: http.MethodPatch, path: "/api/v1/users/current/cycle", okStatus: http.StatusOK,
		body: func(value int) map[string]any {
			return map[string]any{"cycle_length": value, "period_length": 1}
		},
	},
	{
		schema: "CycleSettings", property: "cycle_length", keyword: "maximum",
		method: http.MethodPatch, path: "/api/v1/users/current/cycle", okStatus: http.StatusOK,
		body: func(value int) map[string]any {
			return map[string]any{"cycle_length": value, "period_length": 1}
		},
	},
	{
		schema: "CycleSettings", property: "period_length", keyword: "minimum",
		method: http.MethodPatch, path: "/api/v1/users/current/cycle", okStatus: http.StatusOK,
		// The longest cycle keeps the cross-field rule (period_length <=
		// cycle_length-10, capped at 14) out of the way, so only the bound
		// under probe decides the outcome.
		body: func(value int) map[string]any {
			return map[string]any{"cycle_length": 90, "period_length": value}
		},
	},
	{
		schema: "CycleSettings", property: "period_length", keyword: "maximum",
		method: http.MethodPatch, path: "/api/v1/users/current/cycle", okStatus: http.StatusOK,
		body: func(value int) map[string]any {
			return map[string]any{"cycle_length": 90, "period_length": value}
		},
	},
	{
		schema: "SymptomPayload", property: "name", keyword: "maxLength",
		method: http.MethodPost, path: "/api/v1/symptoms", okStatus: http.StatusCreated,
		body: func(value int) map[string]any {
			return map[string]any{"name": openAPIBoundProbeText(value)}
		},
	},
	{
		schema: "SymptomPayload", property: "icon", keyword: "maxLength",
		method: http.MethodPost, path: "/api/v1/symptoms", okStatus: http.StatusCreated,
		// Symptom names are unique per owner, so each icon probe needs its
		// own name; the name itself is well inside its own bound.
		body: func(value int) map[string]any {
			return map[string]any{"name": "icon probe " + strconv.Itoa(value), "icon": openAPIBoundProbeText(value)}
		},
	},
}

// TestOpenAPIDeclaredBoundsMatchTheServersOwnLimits sweeps every request-shape
// bound the /api/v1 surface enforces and requires docs/openapi.yaml to declare
// it at exactly the value the server uses. A spec looser than the server — a
// declared maximum above the real cap, or no bound at all where one is
// enforced — does not block anything, but it makes the document untrue in the
// direction a client cannot recover from: it cannot tell in advance that a
// value will be refused, so the rejection can only be discovered by sending it.
// It is the mirror image of a spec narrower than the server, which blocks a
// legitimate request outright and is therefore found quickly; this direction is
// found only when someone compares the two by hand, which is what this sweep
// replaces.
//
// Each member is checked the same way, and the expected limits appear nowhere
// in this file: the spec supplies the number, the endpoint judges it. The value
// the spec calls legal must be accepted, and the first value past it must be
// refused — so the declared bound has to be the acceptance boundary itself, not
// merely somewhere near it. Both directions matter: dropping only the negative
// probe would let a spec that understates the cap pass, and dropping only the
// positive one would let a spec that overstates it pass.
//
// The spec previously declared maxLength 80 for display_name where the server
// caps at 64, minimum 1 and no maximum for cycle_length where the server
// accepts 15-90, no maximum for period_length where it accepts 1-14, and no
// length bound at all for a symptom's name or icon (40 and 16 runes).
func TestOpenAPIDeclaredBoundsMatchTheServersOwnLimits(t *testing.T) {
	bounds := openAPISchemaPropertyBounds(t, filepath.Join("..", "..", "docs", "openapi.yaml"))
	if len(bounds) == 0 {
		t.Fatal("no schema property bounds parsed from openapi.yaml; parser or spec is wrong")
	}

	app, database := newOnboardingTestApp(t)
	const email, password = "openapi-bounds@example.com", "StrongPass1"
	createOnboardingTestUser(t, database, email, password, true)
	authCookie := loginAndExtractAuthCookie(t, app, email, password)

	for _, bound := range openAPIDeclaredBounds {
		field := bound.schema + "." + bound.property
		t.Run(field+"/"+bound.keyword, func(t *testing.T) {
			declared, ok := bounds[field][bound.keyword]
			if !ok {
				t.Fatalf("docs/openapi.yaml declares no %s for %s, but %s %s enforces one: a client validating against the spec cannot tell the value will be refused",
					bound.keyword, field, bound.method, bound.path)
			}

			inBounds, outOfBounds := declared, declared+1
			if bound.keyword == "minimum" || bound.keyword == "minLength" {
				outOfBounds = declared - 1
			}

			if status := openAPIProbeBound(t, app, authCookie, bound, inBounds); status != bound.okStatus {
				t.Fatalf("%s declares %s: %d, but %s %s answered %d for a value the spec calls legal (want %d): the spec is looser than the server",
					field, bound.keyword, declared, bound.method, bound.path, status, bound.okStatus)
			}
			if status := openAPIProbeBound(t, app, authCookie, bound, outOfBounds); status < 400 || status > 499 {
				t.Fatalf("%s declares %s: %d, but %s %s answered %d for %d, one past the declared bound (want a 4xx refusal): the spec is tighter than the server",
					field, bound.keyword, declared, bound.method, bound.path, status, outOfBounds)
			}
		})
	}
}

// openAPIProbeBound sends one JSON request built from the bound's body builder
// and returns the status the endpoint answered with.
func openAPIProbeBound(t *testing.T, app *fiber.App, authCookie string, bound openAPIDeclaredBound, value int) int {
	t.Helper()

	payload, err := json.Marshal(bound.body(value))
	if err != nil {
		t.Fatalf("marshal probe body for %s.%s: %v", bound.schema, bound.property, err)
	}
	request := httptest.NewRequest(bound.method, bound.path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Accept", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	return mustAppResponse(t, app, request).StatusCode
}

// openAPIRecoveryCodePatternLine finds the `pattern: "^OVUM-..."` (or
// `pattern: '^OVUM-...'`) line declared for ForgotPasswordRequest.recovery_code.
// It is the only `pattern:` line in the spec starting with "^OVUM-", so a
// plain line scan identifies it unambiguously without needing to track YAML
// nesting. Both quote styles are matched deliberately, each with its own
// capture group scoped to ITS OWN quote character: the pattern's `\s`
// alternative (matching the handler's `strings.TrimSpace(...) == ""` blank
// case) needs a literal backslash in the file's raw bytes, and YAML only
// round-trips that byte-for-byte in a single-quoted scalar — double-quoted
// would require doubling the backslash, and this extractor reads raw bytes
// with no YAML unescaping, so a doubled backslash here would silently compile
// into a different, wrong expression instead of failing loudly.
var openAPIRecoveryCodePatternLine = regexp.MustCompile(`(?m)^\s*pattern:\s*(?:"(\^OVUM-[^"]*)"|'(\^OVUM-[^']*)')\s*$`)

// openAPIRecoveryCodePattern extracts and compiles the `pattern` declared for
// ForgotPasswordRequest.recovery_code in the OpenAPI spec. Like
// openAPIV1Routes above, it scans the raw text rather than pulling in a YAML
// library, matching this file's dependency-free convention.
func openAPIRecoveryCodePattern(t *testing.T, specPath string) *regexp.Regexp {
	t.Helper()
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}

	match := openAPIRecoveryCodePatternLine.FindSubmatch(data)
	if match == nil {
		t.Fatalf(`docs/openapi.yaml: no recovery_code pattern line found (expected pattern: "^OVUM-..." or pattern: '^OVUM-...')`)
	}
	raw := match[1]
	if len(raw) == 0 {
		raw = match[2]
	}

	compiled, err := regexp.Compile(string(raw))
	if err != nil {
		t.Fatalf("docs/openapi.yaml recovery_code pattern %q does not compile: %v", raw, err)
	}
	return compiled
}

// TestOpenAPIRecoveryCodePatternAcceptsGeneratedCodes pins the recovery-code
// request-contract class: docs/openapi.yaml declares a `pattern` for
// ForgotPasswordRequest.recovery_code, and that pattern must accept every
// code services.GenerateRecoveryCode can actually mint, and must classify
// input exactly as services.ValidateRecoveryCodeFormat does — the server's
// own request-shape check (internal/services/auth_input_policy.go), which
// password_reset_service.go runs on every /api/v1/password-resets request. A
// pattern narrower than either rejects a legitimate password-reset attempt
// for any client that validates requests against the spec before sending
// them — the last path back into a locked-out owner's account.
//
// The spec previously declared "^OVUM-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}$"
// (hex digits only), which a real generated code — drawn from a 32-symbol
// Crockford-style alphabet, only 14 of whose symbols are also hex digits —
// matched with probability (14/32)^12 ~= 4.9e-5, i.e. roughly 1 in 20,000.
func TestOpenAPIRecoveryCodePatternAcceptsGeneratedCodes(t *testing.T) {
	specPattern := openAPIRecoveryCodePattern(t, filepath.Join("..", "..", "docs", "openapi.yaml"))

	// Every code the generator can actually mint must pass both the
	// documented pattern and the server's own validator. A single sample
	// is not enough: the generator draws independently per character from
	// its alphabet, so a pattern that rejects only some characters can
	// still pass on a lucky draw. Enough iterations make that negligible.
	for range 500 {
		code, err := services.GenerateRecoveryCode()
		if err != nil {
			t.Fatalf("services.GenerateRecoveryCode: %v", err)
		}
		if !specPattern.MatchString(code) {
			t.Fatalf("docs/openapi.yaml recovery_code pattern %q rejects a real generated code %q", specPattern.String(), code)
		}
		if err := services.ValidateRecoveryCodeFormat(code); err != nil {
			t.Fatalf("services.ValidateRecoveryCodeFormat rejected a real generated code %q: %v", code, err)
		}
	}

	// The documented pattern must also agree with the server's validator on
	// the accepted character class itself, not only on generator output:
	// ValidateRecoveryCodeFormat deliberately accepts a wider class
	// ([A-Z0-9]) than the generator emits (it excludes ambiguous I/O/0/1),
	// so the spec must mirror the SERVER's accepted class, not the
	// generator's narrower alphabet. Check every alphanumeric character in
	// each of the three 4-character groups.
	for _, r := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789" {
		group := strings.Repeat(string(r), 4)
		sample := "OVUM-" + group + "-2222-3333"
		specAccepts := specPattern.MatchString(sample)
		serverAccepts := services.ValidateRecoveryCodeFormat(sample) == nil
		if specAccepts != serverAccepts {
			t.Fatalf("docs/openapi.yaml recovery_code pattern disagrees with services.ValidateRecoveryCodeFormat for character %q: spec accepts=%v, server accepts=%v (sample %q)", r, specAccepts, serverAccepts, sample)
		}
	}
}

// TestOpenAPIRecoveryCodePatternAcceptsTheHandlersBlankCase ties the pattern
// compiled FROM THE SPEC ITSELF to ForgotPassword's actual blank-selects-step-1
// rule: parseForgotPasswordInput reads `rawCode :=
// strings.TrimSpace(input.RecoveryCode)` and treats `rawCode == ""` — omitted,
// empty, or all whitespace — as step 1, never a refusal
// (internal/api/handlers_auth_session_helpers.go). A pattern requiring the
// full OVUM-XXXX-XXXX-XXXX shape unconditionally would make a spec-validating
// client reject its own step-1 body before ever sending it, even though the
// server accepts it; the class this catches previously escaped detection
// entirely because a doubled backslash in a double-quoted YAML scalar let the
// pattern's blank alternative compile to something that matched neither " "
// nor "\t" while still passing every existing assertion, which only fed it
// non-blank codes. A non-blank but malformed code must still be refused.
func TestOpenAPIRecoveryCodePatternAcceptsTheHandlersBlankCase(t *testing.T) {
	specPattern := openAPIRecoveryCodePattern(t, filepath.Join("..", "..", "docs", "openapi.yaml"))

	for _, blank := range []string{"", " ", "\t", "  \t  "} {
		if !specPattern.MatchString(blank) {
			t.Fatalf("docs/openapi.yaml recovery_code pattern %q rejects %q, which ForgotPassword's strings.TrimSpace(...) == \"\" check accepts as step 1", specPattern.String(), blank)
		}
	}

	for _, malformed := range []string{"garbage", "OVUM-ABCD-2345-EFG", "ovum-abcd-2345-efgh"} {
		if specPattern.MatchString(malformed) {
			t.Fatalf("docs/openapi.yaml recovery_code pattern %q wrongly accepts non-blank malformed code %q", specPattern.String(), malformed)
		}
	}
}
