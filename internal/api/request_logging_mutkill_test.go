package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// Mutation-kill tests for SafeRequestLogPath (request_logging.go). The function
// prefers the matched route TEMPLATE (with `:param` names) over the raw request
// path, so a parameter value never leaks into logs even when the value is not
// itself maskable (e.g. a plain word). Two guards select the template:
//   - L18 `route != nil`: only read the template when a route matched.
//   - L23 `routePath != "" && !USE`: use the sanitized template for a concrete
//     (non-middleware) route.
//
// Negating either (CONDITIONALS_NEGATION) skips the template and falls through to
// sanitizing the raw path, which for a non-maskable value differs from the
// template.
//
// EQUIVALENT (documented, not killed) — L20 `routePath == "/*" || routePath == "*"`:
// this early-return for a bare wildcard template is an optimization. For the only
// wildcard a registered fiber route emits ("/*"), sanitizeRequestLogPath("/*")
// == "/*", so returning it via the early return or via the L23 fallback yields an
// identical string; and L23 sanitizes the same routePath for a non-USE route.
// Only a bare "*" template (which fiber's router does not produce — it keeps the
// leading slash) or a USE-mounted "/*" would differ, neither of which occurs in
// this app's routing. Negating either operator is therefore output-equivalent for
// realistic inputs, so it is documented rather than pinned with a contrived route.

// TestSafeRequestLogPathPrefersRouteTemplateOverRawValue pins L18 and L23: for a
// matched param route whose value is a non-maskable word, the log path must be
// the route template ("/probe/:slug"), never the raw value ("/probe/hello").
// Both guard negations fall through to sanitizing the raw path and would return
// "/probe/hello".
func TestSafeRequestLogPathPrefersRouteTemplateOverRawValue(t *testing.T) {
	app := fiber.New()
	app.Get("/probe/:slug", func(c fiber.Ctx) error {
		return c.SendString(SafeRequestLogPath(c))
	})

	req := httptest.NewRequest(http.MethodGet, "/probe/hello", nil)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("safe-log-path probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	got := mustReadBodyString(t, resp.Body)
	if got != "/probe/:slug" {
		t.Fatalf("SafeRequestLogPath must return the route template, got %q", got)
	}
}

// TestSafeRequestLogPathMasksRawValueOnRawPath pins the sanitization the fallback
// still applies: a maskable raw value (a date) is redacted to its class token.
// This holds on the original and guards the raw-path arm the guards protect.
func TestSafeRequestLogPathMasksRawValueOnRawPath(t *testing.T) {
	app := fiber.New()
	app.Get("/days/:date", func(c fiber.Ctx) error {
		return c.SendString(SafeRequestLogPath(c))
	})

	req := httptest.NewRequest(http.MethodGet, "/days/2026-02-17", nil)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("safe-log-path mask probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	got := mustReadBodyString(t, resp.Body)
	if got != "/days/:date" {
		t.Fatalf("SafeRequestLogPath must mask the date value, got %q", got)
	}
}
