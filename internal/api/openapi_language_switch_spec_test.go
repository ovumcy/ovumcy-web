package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
)

// limiterMount is one limiter.New the composition root wires through a Use
// call: the prefix it is mounted on and how its Next filter scopes it. The
// limiters are mounted in cmd/ovumcy, which internal/api cannot import, so they
// are read as source, the same way requireAPIRateLimitMountHasNoNextFilter
// reads the /api mount.
type limiterMount struct {
	source string
	prefix string
	// scoped is set when Next is a rateLimitOnlyFor call: the limiter then
	// counts exactly (method, path) — plus HEAD for a GET — and nothing else.
	scoped bool
	method string
	path   string
	// opaque is set when Next is anything else. Such a limiter may or may not
	// reach a given operation under its prefix, and this reader cannot tell.
	opaque bool
}

// covers reports whether the mount's limiter counts a request for the
// documented operation. The comparison goes through httpx.RoutingNormalizedPath,
// the normalization rateLimitOnlyFor itself applies, so the reader and the
// production predicate agree on what "the same path" means.
func (mount limiterMount) covers(method string, path string) (covered bool, undecidable bool) {
	normalizedPath := httpx.RoutingNormalizedPath(path)
	if mount.prefix != "" && mount.prefix != "/" {
		normalizedPrefix := httpx.RoutingNormalizedPath(mount.prefix)
		if normalizedPath != normalizedPrefix && !strings.HasPrefix(normalizedPath, normalizedPrefix+"/") {
			return false, false
		}
	}
	switch {
	case mount.opaque:
		return false, true
	case mount.scoped:
		methodMatches := method == mount.method || (mount.method == fiber.MethodGet && method == fiber.MethodHead)
		return methodMatches && normalizedPath == httpx.RoutingNormalizedPath(mount.path), false
	default:
		return true, false
	}
}

// parseNonTestGoFiles parses every non-test Go file in dir.
func parseNonTestGoFiles(t *testing.T, fileSet *token.FileSet, dir string) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fileSet, filepath.Join(dir, name), nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		files = append(files, parsed)
	}
	if len(files) == 0 {
		t.Fatalf("no non-test Go file parsed in %s; discovery broke", dir)
	}
	return files
}

// collectStringConstants records every package-level string constant declared
// with a literal value, keyed by qualifier+name, so a limiter's path written as
// a constant (api.LanguageSwitchPath, api.CalendarFeedRateLimitPrefix) resolves
// to the value the router sees without a lookup table kept here.
func collectStringConstants(files []*ast.File, qualifier string, into map[string]string) {
	for _, file := range files {
		for _, decl := range file.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range valueSpec.Names {
					if index >= len(valueSpec.Values) {
						continue
					}
					literal, ok := valueSpec.Values[index].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					if value, err := strconv.Unquote(literal.Value); err == nil {
						into[qualifier+name.Name] = value
					}
				}
			}
		}
	}
}

func resolveStringExpr(expr ast.Expr, constants map[string]string) (string, bool) {
	switch node := expr.(type) {
	case *ast.BasicLit:
		if node.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(node.Value)
		return value, err == nil
	case *ast.Ident:
		value, ok := constants[node.Name]
		return value, ok
	case *ast.SelectorExpr:
		qualifier, ok := node.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		value, ok := constants[qualifier.Name+"."+node.Sel.Name]
		return value, ok
	}
	return "", false
}

// resolveFiberMethod reads a fiber.Method* selector as the verb it names.
func resolveFiberMethod(expr ast.Expr) (string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok || qualifier.Name != "fiber" || !strings.HasPrefix(selector.Sel.Name, "Method") {
		return "", false
	}
	return strings.ToUpper(strings.TrimPrefix(selector.Sel.Name, "Method")), true
}

func isLimiterNewCall(expr ast.Expr) (*ast.CallExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	return call, ok && qualifier.Name == "limiter" && selector.Sel.Name == "New"
}

// discoverLimiterMounts returns every limiter.New mounted through a Use call in
// cmd/ovumcy's non-test sources. A mount whose prefix or Next scope it cannot
// read fails here rather than being skipped: a limiter this reader drops is a
// 429 the sweep below never asks the spec about.
func discoverLimiterMounts(t *testing.T) []limiterMount {
	t.Helper()
	fileSet := token.NewFileSet()
	cmdFiles := parseNonTestGoFiles(t, fileSet, filepath.Join("..", "..", "cmd", "ovumcy"))
	constants := make(map[string]string)
	collectStringConstants(cmdFiles, "", constants)
	collectStringConstants(parseNonTestGoFiles(t, token.NewFileSet(), "."), "api.", constants)

	var mounts []limiterMount
	for _, file := range cmdFiles {
		ast.Inspect(file, func(node ast.Node) bool {
			use, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := use.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Use" {
				return true
			}
			for argIndex, arg := range use.Args {
				newCall, ok := isLimiterNewCall(arg)
				if !ok {
					continue
				}
				mount := limiterMount{source: fileSet.Position(newCall.Pos()).String()}
				if argIndex > 0 {
					prefix, resolved := resolveStringExpr(use.Args[0], constants)
					if !resolved {
						t.Fatalf("%s: the limiter's mount prefix is not a string literal or a package-level string constant; this guard cannot tell which operations it reaches", mount.source)
					}
					mount.prefix = prefix
				}
				mount.readNextFilter(t, newCall, constants)
				mounts = append(mounts, mount)
			}
			return true
		})
	}
	return mounts
}

func (mount *limiterMount) readNextFilter(t *testing.T, newCall *ast.CallExpr, constants map[string]string) {
	t.Helper()
	if len(newCall.Args) == 0 {
		return
	}
	config, ok := newCall.Args[0].(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s: limiter.New's argument is not a limiter.Config literal; this guard cannot read its Next filter", mount.source)
	}
	for _, element := range config.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := field.Key.(*ast.Ident); !ok || key.Name != "Next" {
			continue
		}
		scope, ok := field.Value.(*ast.CallExpr)
		if callee, isIdent := scopeCallee(scope, ok); !isIdent || callee != "rateLimitOnlyFor" || len(scope.Args) != 2 {
			mount.opaque = true
			return
		}
		method, methodOK := resolveFiberMethod(scope.Args[0])
		path, pathOK := resolveStringExpr(scope.Args[1], constants)
		if !methodOK || !pathOK {
			t.Fatalf("%s: rateLimitOnlyFor's arguments are not a fiber.Method* selector and a string literal or constant; this guard cannot read the limiter's scope", mount.source)
		}
		mount.scoped, mount.method, mount.path = true, method, path
		return
	}
}

func scopeCallee(scope *ast.CallExpr, isCall bool) (string, bool) {
	if !isCall {
		return "", false
	}
	ident, ok := scope.Fun.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// limiterCoveredDocumentedOperationsOutsideV1 returns the operations
// docs/openapi.yaml documents outside /api/v1 that a mounted limiter counts,
// each mapped to the mount that covers it. /api/v1 is left to the route-table
// sweep in TestOpenAPIDeclaresRateLimitedOnEveryLimiterCoveredOperation; every
// other documented operation is decided here, from the mounts themselves, so a
// limiter wired later onto a documented path is swept with no edit to this file.
func limiterCoveredDocumentedOperationsOutsideV1(t *testing.T, declared map[int][]string) map[string]string {
	t.Helper()
	mounts := discoverLimiterMounts(t)
	if len(mounts) == 0 {
		t.Fatal("no limiter.New mount discovered in cmd/ovumcy; the reader, not the wiring, is broken")
	}

	documented := make(map[string]struct{})
	for _, operations := range declared {
		for _, operation := range operations {
			documented[operation] = struct{}{}
		}
	}

	covered := make(map[string]string)
	var undecidable []string
	for operation := range documented {
		method, path, ok := strings.Cut(operation, " ")
		if !ok || strings.HasPrefix(path, "/api/v1") {
			continue
		}
		for _, mount := range mounts {
			isCovered, cannotTell := mount.covers(method, path)
			if cannotTell {
				undecidable = append(undecidable, fmt.Sprintf("%s (limiter at %s)", operation, mount.source))
				continue
			}
			if isCovered {
				covered[operation] = mount.source
			}
		}
	}
	if len(undecidable) > 0 {
		sort.Strings(undecidable)
		t.Fatalf("a limiter whose Next filter is not a rateLimitOnlyFor call is mounted on a prefix that reaches a documented operation, so this guard cannot say whether its 429 is real there — scope the limiter with rateLimitOnlyFor:\n  %s",
			strings.Join(undecidable, "\n  "))
	}
	return covered
}

// openAPIYAMLBlock returns the lines nested under the key path given, one key
// per level at two-space steps from column 0 (for example "paths", "/lang",
// "post", "responses", "'400'"). It fails when a level is missing, so a caller
// asking for a response the spec does not declare reddens by name.
func openAPIYAMLBlock(t *testing.T, spec string, keys ...string) []string {
	t.Helper()
	level := 0
	var block []string
	for _, raw := range strings.Split(spec, "\n") {
		line := strings.TrimRight(raw, "\r")
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if level == len(keys) {
			if indent <= 2*(level-1) {
				return block
			}
			block = append(block, text)
			continue
		}
		if indent < 2*level {
			break
		}
		if indent == 2*level && text == keys[level]+":" {
			level++
		}
	}
	if level == len(keys) {
		return block
	}
	t.Fatalf("docs/openapi.yaml declares no %s", strings.Join(keys, " → "))
	return nil
}

func requireSpecLine(t *testing.T, block []string, want string, where string) {
	t.Helper()
	for _, line := range block {
		if line == want {
			return
		}
	}
	t.Errorf("%s: docs/openapi.yaml does not carry %q — the spec no longer describes what the server answers there:\n  %s",
		where, want, strings.Join(block, "\n  "))
}

// TestOpenAPILanguageSwitchDeclaresTheRefusalsItAnswers pins the two refusals
// POST /lang answers a JSON caller against what docs/openapi.yaml declares for
// that operation. The spec published only the 200 and the 303, while the
// handler refuses a blank `lang` with 400 and the route carries a per-IP
// limiter of its own that refuses with 429.
//
// Both answers are produced, not restated: the 400 by SetLanguage behind the
// transport-envelope error handler cmd/ovumcy installs (every *fiber.Error goes
// through RespondTransportError; TestLanguageSwitchRejectionAnswersThroughTheEnvelope
// pins that wiring on the real stack), the 429 by a real fiber limiter whose
// refusal is RespondAPIRateLimited, the responder newAPIRateLimitHandler calls
// for this route. The key, category and target each body carries are then
// required, verbatim, in the example the spec declares for that status.
func TestOpenAPILanguageSwitchDeclaresTheRefusalsItAnswers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	spec := string(data)

	handler := &Handler{}
	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) {
			return RespondTransportError(c, fiberErr.Code)
		}
		return RespondTransportError(c, fiber.StatusInternalServerError)
	}})
	app.Use(limiter.New(limiter.Config{
		Max:          1,
		Expiration:   time.Minute,
		LimitReached: handler.RespondAPIRateLimited,
	}))
	app.Post(LanguageSwitchPath, handler.SetLanguage)

	send := func() (int, http.Header, map[string]any) {
		request := httptest.NewRequest(http.MethodPost, LanguageSwitchPath, strings.NewReader(url.Values{"lang": {"  "}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		response, err := app.Test(request)
		if err != nil {
			t.Fatalf("POST %s: %v", LanguageSwitchPath, err)
		}
		defer func() { _ = response.Body.Close() }()
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("POST %s answered %d with a body that is not JSON: %v", LanguageSwitchPath, response.StatusCode, err)
		}
		if contentType := response.Header.Get(fiber.HeaderContentType); !strings.HasPrefix(contentType, fiber.MIMEApplicationJSON) {
			t.Fatalf("POST %s answered %d as %q, not application/json", LanguageSwitchPath, response.StatusCode, contentType)
		}
		return response.StatusCode, response.Header, body
	}

	envelopeLines := func(status int, body map[string]any) []string {
		detail, _ := body["error_detail"].(map[string]any)
		key, _ := detail["key"].(string)
		category, _ := detail["category"].(string)
		target, _ := detail["target"].(string)
		if key == "" || category == "" || target == "" || body["error"] != key {
			t.Fatalf("POST %s answered %d without the shared error envelope: %v", LanguageSwitchPath, status, body)
		}
		return []string{
			fmt.Sprintf("error: %q", key),
			fmt.Sprintf("error_detail: { key: %q, category: %q, target: %q }", key, category, target),
		}
	}

	status, _, body := send()
	if status != http.StatusBadRequest {
		t.Fatalf("a blank lang answered %d, want 400", status)
	}
	badRequest := openAPIYAMLBlock(t, spec, "paths", LanguageSwitchPath, "post", "responses", "'400'")
	requireSpecLine(t, badRequest, "schema: { $ref: '#/components/schemas/ApiError' }", "POST /lang 400")
	for _, line := range envelopeLines(status, body) {
		requireSpecLine(t, badRequest, line, "POST /lang 400")
	}

	status, header, body := send()
	if status != http.StatusTooManyRequests {
		t.Fatalf("the request past the limiter's budget answered %d, want 429", status)
	}
	if header.Get(fiber.HeaderRetryAfter) == "" {
		t.Fatal("the 429 carries no Retry-After header")
	}
	if seconds, ok := body["retry_after_seconds"].(float64); !ok || seconds < 1 {
		t.Fatalf("the 429 body carries no retry_after_seconds: %v", body)
	}
	rateLimited := openAPIYAMLBlock(t, spec, "paths", LanguageSwitchPath, "post", "responses", "'429'")
	requireSpecLine(t, rateLimited, "$ref: '#/components/responses/RateLimited'", "POST /lang 429")
	component := openAPIYAMLBlock(t, spec, "components", "responses", "RateLimited")
	requireSpecLine(t, component, "Retry-After:", "components.responses.RateLimited")
	requireSpecLinePrefix(t, component, "retry_after_seconds:", "components.responses.RateLimited")
	requireSpecLine(t, component, "schema: { $ref: '#/components/schemas/ApiError' }", "components.responses.RateLimited")
	for _, line := range envelopeLines(status, body) {
		requireSpecLine(t, component, line, "components.responses.RateLimited")
	}
}

func requireSpecLinePrefix(t *testing.T, block []string, prefix string, where string) {
	t.Helper()
	for _, line := range block {
		if strings.HasPrefix(line, prefix) {
			return
		}
	}
	t.Errorf("%s: docs/openapi.yaml carries no line starting %q — the spec no longer describes what the server answers there:\n  %s",
		where, prefix, strings.Join(block, "\n  "))
}
