package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// The export range arrives two ways — a GET with `?from=&to=` and a POST with a
// form body — and `exportRangeInputValues` resolves both with one call each.
// That is not an economy, it is a property of the framework: fiber v3's
// DefaultReq.FormValue searches QueryArgs, then PostArgs, then the multipart
// form (req.go), delegating to fasthttp's defaultFormValue, which peeks
// QueryArgs first (server.go). A query-supplied value therefore comes back from
// FormValue, and an empty FormValue means the query held nothing either.
//
// The helper used to carry an explicit `if from == "" { from = c.Query("from") }`
// fallback behind each lookup. Given the order above those two lines could only
// ever reassign the empty string over itself, so mutation runs reported them as
// survivors and every reader of the export path had to re-derive that they were
// inert. They are gone, and this file is what makes their absence safe: the
// resolution order they were compensating for is a dependency's behaviour, not
// ours, and a fiber upgrade that moved the enforcement point would otherwise
// break the GET export range with nothing saying so.
//
// This is a framework-seam pin, the same shape as
// TestFiberSignalsDecompressedOverflowByStampingTheResponseStatus. It replaces
// the mutation-kill file that used to sit here, whose subject was the two
// conditionals the removal deleted.

// exportRangeProbeResult carries what one probe request observed, so a case can
// assert on the helper's answer and on the two raw lookups behind it.
type exportRangeProbeResult struct {
	from      string
	to        string
	formFrom  string
	queryFrom string
}

// exportRangeProbe drives exportRangeInputValues through a mounted route, so the
// real fiber resolution is exercised rather than a reconstruction of it.
func exportRangeProbe(t *testing.T, method string, target string, body string) exportRangeProbeResult {
	t.Helper()

	app := fiber.New()
	probe := func(c fiber.Ctx) error {
		from, to := exportRangeInputValues(c)
		return c.SendString(strings.Join([]string{from, to, c.FormValue("from"), c.Query("from")}, "\x1f"))
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

	parts := strings.Split(mustReadBodyString(t, resp.Body), "\x1f")
	if len(parts) != 4 {
		t.Fatalf("unexpected probe body shape %q", parts)
	}
	return exportRangeProbeResult{from: parts[0], to: parts[1], formFrom: parts[2], queryFrom: parts[3]}
}

// TestExportRangeResolvesAQueryOnlyRangeThroughOneLookup is the seam. The CSV
// and summary exports are reachable by GET, so this is the live path for a
// range typed into a URL; it holds only because FormValue searches the query
// string before the body.
func TestExportRangeResolvesAQueryOnlyRangeThroughOneLookup(t *testing.T) {
	observed := exportRangeProbe(t, http.MethodGet, "/exprobe?from=2026-05-05&to=2026-05-25", "")

	if observed.from != "2026-05-05" || observed.to != "2026-05-25" {
		t.Fatalf("a query-supplied export range must resolve, got from=%q to=%q — "+
			"fiber's FormValue no longer searches the query string, so the export range needs its own c.Query lookup back",
			observed.from, observed.to)
	}
}

// TestExportRangeResolvesAPostedRangeWithNoQueryCounterpart is the other half:
// a body-supplied range must survive, which is what the deleted fallback would
// have overwritten with the empty query value had its guard ever been negated.
func TestExportRangeResolvesAPostedRangeWithNoQueryCounterpart(t *testing.T) {
	body := url.Values{"from": {"2026-02-01"}, "to": {"2026-02-28"}}.Encode()
	observed := exportRangeProbe(t, http.MethodPost, "/exprobe", body)

	if observed.from != "2026-02-01" || observed.to != "2026-02-28" {
		t.Fatalf("a posted export range must resolve, got from=%q to=%q", observed.from, observed.to)
	}
}

// TestExportRangeQueryLookupAddsNothingFormValueDidNotAlreadySee is the
// measurement the removal rests on, stated as a proposition rather than as a
// claim about the dependency's source: across every shape a request can take,
// a blank FormValue implies a blank Query, so the deleted fallback could only
// assign "" over "". A fiber release that broke the implication reddens here
// and names the shape it broke on.
func TestExportRangeQueryLookupAddsNothingFormValueDidNotAlreadySee(t *testing.T) {
	cases := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "query only", method: http.MethodGet, target: "/exprobe?from=2026-05-05", body: ""},
		{name: "body only", method: http.MethodPost, target: "/exprobe", body: url.Values{"from": {"2026-02-01"}}.Encode()},
		{name: "body and query disagree", method: http.MethodPost, target: "/exprobe?from=2026-03-03", body: url.Values{"from": {"2026-02-01"}}.Encode()},
		{name: "query key present but empty", method: http.MethodGet, target: "/exprobe?from=", body: ""},
		{name: "query value is whitespace", method: http.MethodGet, target: "/exprobe?from=%20", body: ""},
		{name: "empty query value beside a posted one", method: http.MethodPost, target: "/exprobe?from=", body: url.Values{"from": {"2026-02-01"}}.Encode()},
		{name: "nothing at all", method: http.MethodGet, target: "/exprobe", body: ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			observed := exportRangeProbe(t, testCase.method, testCase.target, testCase.body)

			if strings.TrimSpace(observed.formFrom) == "" && strings.TrimSpace(observed.queryFrom) != "" {
				t.Fatalf("c.Query answered %q where c.FormValue answered %q: the query fallback this helper dropped was not inert after all, and the export range now loses a value",
					observed.queryFrom, observed.formFrom)
			}
			if observed.from != strings.TrimSpace(observed.formFrom) {
				t.Fatalf("the helper resolved %q where its single lookup answered %q", observed.from, observed.formFrom)
			}
		})
	}
}
