package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The onboarding gate (services.ShouldEnforceOnboardingAccess, called from
// AuthRequired) exempts the onboarding page and the onboarding API by path
// prefix, and `/api/v1/onboarding/../days/2026-03-01` carries that prefix. Its
// safety used to rest on the router taking the traversal segment literally, so
// the request matched no route — an assumption about a dependency, and one a
// Fiber release that normalizes the request path before matching breaks: the
// router then dispatches the request to the days handler.
//
// The gate no longer leans on it. It decides on the path the router dispatches
// on, and refuses any request whose routed path is not canonical or is not the
// path the request arrived with. This test drives the real middleware and the
// real routes, so its plain-dot rows hold on a router that takes the segment
// literally and on one that removes it; the escaped row assumes the pinned
// router, which decodes "%2e" (fiber 3.6+). It fails if the gate ever goes back
// to reading the raw prefix.
func TestOnboardingGateRefusesATraversalSegmentWhateverTheRouterMakesOfIt(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "gate-traversal@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	get := func(target string) (int, string, string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Cookie", authCookie)
		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("app.Test(%q): %v", target, err)
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read body for %q: %v", target, err)
		}
		return response.StatusCode, string(body), response.Header.Get("Location")
	}

	// Positive anchors. The plain days path is refused by the gate, so the
	// refusal below is recognisable as the gate's; the onboarding page is
	// served, so the refusal is not a gate that refuses everything.
	status, body, _ := get("/api/v1/days/2026-03-01")
	if status != http.StatusForbidden || !strings.Contains(body, "onboarding required") {
		t.Fatalf("fixture anchor: the plain days path must be refused by the onboarding gate, got %d %q", status, body)
	}
	if status, _, _ := get("/onboarding"); status != http.StatusOK {
		t.Fatalf("fixture anchor: the onboarding page must be served to an owner still onboarding, got %d", status)
	}

	for _, traversal := range []string{
		"/api/v1/onboarding/../days/2026-03-01",
		"/api/v1/onboarding/./../days/2026-03-01",
	} {
		status, body, _ := get(traversal)
		if status != http.StatusForbidden || !strings.Contains(body, "onboarding required") {
			t.Fatalf("%q must be refused by the onboarding gate (403 onboarding required) whether the router "+
				"takes the traversal segment literally or dispatches it to the days route; got %d %q",
				traversal, status, body)
		}
	}

	// An escaped traversal is decoded onto the days route by the router; the
	// gate must refuse it, and only a gate that read the wire path instead of
	// the routed one would let it through.
	const escaped = "/api/v1/onboarding/%2e%2e/days/2026-03-01"
	status, body, _ = get(escaped)
	if status != http.StatusForbidden || !strings.Contains(body, "onboarding required") {
		t.Fatalf("%q must be refused by the onboarding gate (403 onboarding required); got %d %q", escaped, status, body)
	}

	// The gate compares the path alone: a query string neither unlocks a gated
	// route by naming an exempt one nor locks an exempt route out.
	if status, _, _ := get("/onboarding?step=2"); status != http.StatusOK {
		t.Fatalf("an exempt path with a query string must still be served, got %d", status)
	}
	status, body, _ = get("/api/v1/days/2026-03-01?next=/api/v1/onboarding")
	if status != http.StatusForbidden || !strings.Contains(body, "onboarding required") {
		t.Fatalf("a gated path whose query names an exempt route must be refused by the gate; got %d %q", status, body)
	}
}

// A request the router would dispatch to an exempt route is still refused when
// its path had to be normalized to get there: no page or form of this app
// produces such a path, so the gate treats it as illegitimate rather than
// guessing what it meant.
func TestOnboardingGateRefusesAPathTheRouterHadToNormalize(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "gate-normalized@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	get := func(target string) (int, string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Cookie", authCookie)
		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("app.Test(%q): %v", target, err)
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read body for %q: %v", target, err)
		}
		return response.StatusCode, string(body)
	}

	// Positive anchor: the canonical spelling of the same exempt prefix passes
	// the gate (it reaches no GET handler, so it ends in the router's answer,
	// not the gate's), so the refusal below is the dot segment's alone.
	if status, body := get("/api/v1/onboarding/steps/1"); status == http.StatusForbidden || strings.Contains(body, "onboarding required") {
		t.Fatalf("fixture anchor: the canonical onboarding step path must pass the gate, got %d %q", status, body)
	}

	for _, normalized := range []string{
		"/api/v1/onboarding/./steps/1",
		"/api/v1/onboarding//steps/1",
	} {
		status, body := get(normalized)
		if status != http.StatusForbidden || !strings.Contains(body, "onboarding required") {
			t.Fatalf("%q must be refused by the onboarding gate (403 onboarding required) although it names an exempt route; got %d %q",
				normalized, status, body)
		}
	}
}
