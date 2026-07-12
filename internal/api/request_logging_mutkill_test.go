package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestSanitizeRequestLogSegmentRedactsBoundaryLetterTokens pins the UPPER bounds
// of the two letter ranges in isOpaqueRequestLogSegment: L155 `char <= 'z'` and
// L156 `char <= 'Z'`. The table in TestIsOpaqueRequestLogSegment covers the lower
// bounds ('a'/'A') and every out-of-range witness, but NO accepted-token row
// contains a literal 'z' or 'Z' — so a CONDITIONALS_BOUNDARY shift of either upper
// bound to `< 'z'` / `< 'Z'` survives that table (empirically confirmed: the whole
// request_logging suite still passes with both shifts applied).
//
// The shift is a redaction LOOSENING, not a formatting nit. Opaque bearer tokens
// (e.g. the calendar-feed capability token) use a base64url-style alphabet, so
// 'z' and 'Z' are in-range token characters. Under the shifted bound a token that
// happens to contain the boundary letter fails isOpaqueRequestLogSegment, so
// sanitizeRequestLogSegment falls through to its `default` arm and writes the
// token VALUE verbatim into the always-on request log — the exact leak the log
// redaction path exists to prevent.
//
// Each row is a 24-char (the minimum opaque length) token whose only
// boundary-letter is 'z' (isolates L155) or 'Z' (isolates L156); both must stay
// classified as ":token".
func TestSanitizeRequestLogSegmentRedactsBoundaryLetterTokens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		segment string
	}{
		{"upper lowercase boundary z stays redacted", strings.Repeat("a", 23) + "z"},
		{"upper uppercase boundary Z stays redacted", strings.Repeat("A", 23) + "Z"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !isOpaqueRequestLogSegment(tc.segment) {
				t.Fatalf("isOpaqueRequestLogSegment(%q) = false, want true: the boundary letter must stay in the accepted range", tc.segment)
			}
			if got := sanitizeRequestLogSegment(tc.segment); got != ":token" {
				t.Fatalf("sanitizeRequestLogSegment(%q) = %q, want %q: a boundary-letter token must be redacted, not logged verbatim", tc.segment, got, ":token")
			}
		})
	}
}
