package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// Mutation-kill tests for exportRangeInputValues (handlers_export_request_helpers.go
// L53/L56). The helper prefers the posted form value and only falls back to the
// query string when the form value is empty (`if from == ""` / `if to == ""`).
// Negating either guard (CONDITIONALS_NEGATION) makes the code overwrite a
// present form value with the query value, so a form-supplied range with no
// query counterpart is silently discarded (replaced by the empty query string).
//
// Note on the harness: fiber v3's c.FormValue already merges the query string
// (query wins), so a form-vs-query *conflict* cannot be constructed via app.Test.
// The distinguishing input is therefore a body-only value with NO query param:
// FormValue returns the body value while Query returns "" — negating the guard
// overwrites the body value with that empty string.

// exportRangeInputValuesForTest drives exportRangeInputValues via a mounted
// route so the real fiber FormValue/Query resolution is exercised, and returns
// the resolved "from|to" pair.
func exportRangeInputValuesForTest(t *testing.T, method string, target string, body string) (string, string) {
	t.Helper()

	app := fiber.New()
	probe := func(c fiber.Ctx) error {
		from, to := exportRangeInputValues(c)
		return c.SendString(from + "|" + to)
	}
	app.Get("/exprobe", probe)
	app.Post("/exprobe", probe)

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("export range probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	parts := strings.SplitN(mustReadBodyString(t, resp.Body), "|", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected probe body shape %q", parts)
	}
	return parts[0], parts[1]
}

// TestExportRangeInputValuesPreservesFormValue pins L53 and L56: a body-supplied
// value with no query counterpart must be preserved, not overwritten by the
// empty query fallback. Negating either guard replaces the value with "".
func TestExportRangeInputValuesPreservesFormValue(t *testing.T) {
	body := url.Values{"from": {"2026-02-01"}, "to": {"2026-02-28"}}.Encode()
	from, to := exportRangeInputValuesForTest(t, http.MethodPost, "/exprobe", body)

	if from != "2026-02-01" {
		t.Fatalf("form-supplied from must be preserved (L53), got %q", from)
	}
	if to != "2026-02-28" {
		t.Fatalf("form-supplied to must be preserved (L56), got %q", to)
	}
}

// TestExportRangeInputValuesQueryFallbackWhenFormEmpty pins the fallback arm the
// guards protect: with no form body, the query values are adopted. This holds on
// the original for both guards and documents the fallback contract.
func TestExportRangeInputValuesQueryFallbackWhenFormEmpty(t *testing.T) {
	from, to := exportRangeInputValuesForTest(t, http.MethodGet, "/exprobe?from=2026-05-05&to=2026-05-25", "")

	if from != "2026-05-05" {
		t.Fatalf("query from must be adopted when form empty, got %q", from)
	}
	if to != "2026-05-25" {
		t.Fatalf("query to must be adopted when form empty, got %q", to)
	}
}
