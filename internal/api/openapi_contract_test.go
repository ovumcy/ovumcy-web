package api

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// openAPIRecoveryCodePatternLine finds the `pattern: "^OVUM-..."` line
// declared for ForgotPasswordRequest.recovery_code. It is the only `pattern:`
// line in the spec starting with "^OVUM-", so a plain line scan identifies it
// unambiguously without needing to track YAML nesting.
var openAPIRecoveryCodePatternLine = regexp.MustCompile(`(?m)^\s*pattern:\s*"(\^OVUM-[^"]*)"\s*$`)

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
		t.Fatalf(`docs/openapi.yaml: no recovery_code pattern line found (expected pattern: "^OVUM-...")`)
	}

	compiled, err := regexp.Compile(string(match[1]))
	if err != nil {
		t.Fatalf("docs/openapi.yaml recovery_code pattern %q does not compile: %v", match[1], err)
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
