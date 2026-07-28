package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
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
		routes[route.Method+" "+fiberPathToOpenAPI(route.Path)] = struct{}{}
	}
	return routes
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
