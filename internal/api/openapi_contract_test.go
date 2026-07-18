package api

import (
	"os"
	"path/filepath"
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
