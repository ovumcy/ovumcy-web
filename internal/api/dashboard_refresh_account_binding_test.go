package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// dashboardStatusHeaderHook is the block the journal's refresh paints into the
// page it already shows: a response carrying it is the dashboard.
const dashboardStatusHeaderHook = "data-dashboard-status-header"

// dashboardRoutePaths finds the routes that serve the dashboard FROM THE ROUTER,
// not from a list: every parameter-free GET route, asked as the signed-in
// account with no binding (which the guard never judges), and answering 200 with
// the status header the refresh swaps. A route that serves the dashboard later
// is held to the guard without anyone naming it here. The two paths the
// application serves it on are asserted by name so a sweep that reached nothing
// cannot pass.
func dashboardRoutePaths(t *testing.T, fixture dayFormAccountFixture) []string {
	t.Helper()

	var paths []string
	seen := map[string]bool{}
	for _, route := range fixture.app.GetRoutes(true) {
		if route.Method != fiber.MethodGet || strings.ContainsAny(route.Path, ":*+") || seen[route.Path] {
			continue
		}
		seen[route.Path] = true
		request := httptest.NewRequest(http.MethodGet, route.Path, nil)
		request.Header.Set("Cookie", fixture.secondCookie)
		request.Header.Set("Accept", "text/html")
		response := mustAppResponse(t, fixture.app, request)
		body := mustReadBodyString(t, response.Body)
		if response.StatusCode == http.StatusOK && strings.Contains(body, dashboardStatusHeaderHook) {
			paths = append(paths, route.Path)
		}
	}
	slices.Sort(paths)
	for _, required := range []string{"/", "/dashboard"} {
		if !slices.Contains(paths, required) {
			t.Fatalf("the router sweep lost %s — the routes that serve the dashboard resolved to %v", required, paths)
		}
	}
	return paths
}

// refreshRequest is the journal's refresh as the browser sends it: a plain page
// fetch (no HX-Request) that names the account the page was rendered for in the
// binding header, one header line per value.
func refreshRequest(path, cookie string, bindings ...string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Cookie", cookie)
	request.Header.Set("Accept", "text/html")
	for _, binding := range bindings {
		request.Header.Add(dayFormAccountWireHeader, binding)
	}
	return request
}

// TestDashboardRefreshFromAPageOfAnotherAccountIsRefusedAndSendsNothing drives
// the stale-tab hazard on the dashboard's own read: a tab rendered for the first
// account refreshes its status header after the second signed in in another
// tab. The page the second account's cookie would be served never leaves the
// server.
func TestDashboardRefreshFromAPageOfAnotherAccountIsRefusedAndSendsNothing(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "dashboard-refresh")
	paths := dashboardRoutePaths(t, fixture)

	for _, path := range paths {
		for name, bindings := range map[string][]string{
			"the first account's binding":              {fixture.firstBinding},
			"a valid copy beside one that is not":      {fixture.secondBinding, fixture.firstBinding},
			"a copy that is not the binding of anyone": {"not-a-binding"},
			"a header that names no account":           {""},
		} {
			response := mustAppResponse(t, fixture.app, refreshRequest(path, fixture.secondCookie, bindings...))
			body := assertDashboardRefreshRefusal(t, "GET "+path+" with "+name, response)
			for _, leaked := range []string{fixture.secondBinding, dashboardStatusHeaderHook, "data-dashboard-save-form"} {
				if strings.Contains(body, leaked) {
					t.Fatalf("GET %s with %s: the refusal carries the second account's page (%q): %q", path, name, leaked, body)
				}
			}
		}
	}
}

// assertDashboardRefreshRefusal requires the 409 of the day form page
// refusal as a non-HTMX client sees it: the JSON envelope under the refusal's
// own key.
func assertDashboardRefreshRefusal(t *testing.T, label string, response *http.Response) string {
	t.Helper()
	body := mustReadBodyString(t, response.Body)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("%s: must be refused 409, got %d %q", label, response.StatusCode, body)
	}
	if !strings.Contains(body, `"error":"day form page account changed"`) {
		t.Fatalf("%s: the 409 must carry the day form page refusal key, got %q", label, body)
	}
	return body
}

// TestDashboardRefreshFromTheRenderedAccountIsAnswered is the positive anchor
// of the refusal above: the same request, sent by the account that is signed in,
// is answered with its own page.
func TestDashboardRefreshFromTheRenderedAccountIsAnswered(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "dashboard-refresh-own")

	for _, path := range dashboardRoutePaths(t, fixture) {
		for _, account := range []struct{ cookie, binding string }{
			{fixture.firstCookie, fixture.firstBinding},
			{fixture.secondCookie, fixture.secondBinding},
		} {
			response := mustAppResponse(t, fixture.app, refreshRequest(path, account.cookie, account.binding))
			assertStatusCode(t, response, http.StatusOK)
			body := mustReadBodyString(t, response.Body)
			if !strings.Contains(body, dashboardStatusHeaderHook) || !strings.Contains(body, account.binding) {
				t.Fatalf("GET %s with its own binding must be answered with its own dashboard", path)
			}
		}
	}
}

// TestDashboardNavigationWithoutTheBindingIsAnsweredAsBefore pins what the guard
// must not touch: opening the dashboard by its URL, a reload, a link, a form's
// GET and an HTMX boost carry no binding and are answered with the page of the
// account that is signed in — whichever account that is, the HTMX ones included,
// where the calendar's reads would be refused.
func TestDashboardNavigationWithoutTheBindingIsAnsweredAsBefore(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "dashboard-navigation")

	for _, path := range dashboardRoutePaths(t, fixture) {
		for name, header := range map[string]map[string]string{
			"a navigation":             {"Accept": "text/html"},
			"a request with no Accept": {},
			"an HTMX boost":            {"Accept": "text/html", "HX-Request": "true", "HX-Boosted": "true"},
			"an HTMX request":          {"HX-Request": "true"},
			"a JSON client":            {"Accept": "application/json"},
		} {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Cookie", fixture.secondCookie)
			for key, value := range header {
				request.Header.Set(key, value)
			}
			response := mustAppResponse(t, fixture.app, request)
			assertStatusCode(t, response, http.StatusOK)
			body := mustReadBodyString(t, response.Body)
			if !strings.Contains(body, fixture.secondBinding) {
				t.Fatalf("GET %s as %s must be answered with the signed-in account's own page", path, name)
			}
		}
	}
}

// TestDashboardRefusalIsAuditedOnlyWhenTheBindingWasPresented holds the
// refusal to the record the calendar's read refusal leaves, and the answered
// requests to leaving none: a navigation is not an access attempt, so it never
// reads as one in the stream. Not parallel: the audit stream is the
// process-wide log writer.
func TestDashboardRefusalIsAuditedOnlyWhenTheBindingWasPresented(t *testing.T) {
	fixture := newDayFormAccountFixtureWithOptions(t, "dashboard-refresh-audit", onboardingTestAppOptions{auditLogEnabled: true})

	for _, path := range []string{"/", "/dashboard"} {
		response, logOutput := captureAuditedRequest(t, fixture.app, refreshRequest(path, fixture.secondCookie, fixture.firstBinding))
		assertDashboardRefreshRefusal(t, "GET "+path, response)
		line := securityEventLine(t, logOutput, dayFormPageReadAction, "denied")
		if !strings.Contains(line, `reason="day form page account changed"`) {
			t.Fatalf("GET %s: the refusal must be audited with its reason, got %q", path, line)
		}

		for name, request := range map[string]*http.Request{
			"a navigation": refreshRequest(path, fixture.secondCookie),
			"an HTMX boost": func() *http.Request {
				boost := refreshRequest(path, fixture.secondCookie)
				boost.Header.Set("HX-Request", "true")
				boost.Header.Set("HX-Boosted", "true")
				return boost
			}(),
			"the same account's refresh": refreshRequest(path, fixture.secondCookie, fixture.secondBinding),
		} {
			response, logOutput := captureAuditedRequest(t, fixture.app, request)
			assertStatusCode(t, response, http.StatusOK)
			if strings.Contains(logOutput, dayFormPageReadAction) || strings.Contains(logOutput, "day form page account changed") {
				t.Fatalf("GET %s as %s answered the page and must leave no refusal in the audit stream, got %q", path, name, logOutput)
			}
		}
	}
}
