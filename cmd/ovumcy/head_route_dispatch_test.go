package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/db"
)

// head_route_dispatch_test.go pins what a HEAD request reaches on the REAL
// production route table — the one newFiberApp assembles, where the terminal
// app.Use(handler.NotFound) is registered after api.RegisterRoutes.
//
// The distinction matters because fiber does not create a GET route's HEAD
// counterpart when the route is registered: it appends the copies at startup
// (App.startupProcess), by which time every directly-registered Use middleware
// already occupies the HEAD stack. A copy appended behind the catch-all is
// never reached, so HEAD to a page or /api/v1 route answered 404 while GET
// answered the route. An app built by internal/api's own test helper cannot
// see that: it mounts no catch-all.
//
// headRefusalMarker is the one deliberate exception, resolved from the route
// table instead of being re-listed here: a route whose GET spends a one-time
// reveal refuses HEAD, so the HEAD twin cannot burn a secret whose body the
// protocol then discards. Which routes those are is declared in
// internal/api/routes.go and pinned against the table there
// (TestShownOnceGETRoutesAreExactlyTheDeclaredSet); this file only reads the
// chain and holds every other route to GET's own answer.
const headRefusalMarker = "refuseHEADOnShownOnceSurface"

// newHeadDispatchTestApp builds the production app over a handler with a
// database, with every rate limit high enough that the probes here cannot trip
// one and be mistaken for the dispatch defect this file exists to catch.
func newHeadDispatchTestApp(t *testing.T) (*fiber.App, string) {
	t.Helper()

	handler, database := newRateLimitTestHandlerAndDB(t)
	user := seedOwner(t, db.NewRepositories(database), "head-dispatch@example.com", 14)
	token := armCalendarFeedToken(t, database, user.ID)
	return newCalendarFeedTestApp(t, handler, 100000), api.CalendarFeedRateLimitPrefix + "/" + token + ".ics"
}

// headDispatchCase is one (path, expected GET status) probe. The GET status is
// declared rather than copied from the GET response so a case cannot go green
// by mirroring two identical failures.
type headDispatchCase struct {
	name       string
	path       string
	accept     string
	wantStatus int
	assertHEAD func(t *testing.T, response *http.Response)
}

// TestHeadIsAnsweredByTheRouteHandlerNotTheCatchAll drives HEAD against routes
// of every posture the table holds — an unauthenticated probe, a public page,
// the token-authenticated .ics feed, a JSON API route behind AuthRequired — and
// requires each to answer with its own route's status, the one GET answers.
// The unknown-path case is the other half: there HEAD must still reach the
// catch-all and answer exactly as GET does.
func TestHeadIsAnsweredByTheRouteHandlerNotTheCatchAll(t *testing.T) {
	app, feedTarget := newHeadDispatchTestApp(t)

	cases := []headDispatchCase{
		{
			name:       "liveness probe",
			path:       "/healthz",
			wantStatus: http.StatusOK,
		},
		{
			name:       "public page",
			path:       "/login",
			wantStatus: http.StatusOK,
		},
		{
			name:       "armed calendar feed",
			path:       feedTarget,
			wantStatus: http.StatusOK,
			assertHEAD: func(t *testing.T, response *http.Response) {
				t.Helper()

				if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/calendar") {
					t.Fatalf("expected HEAD on the armed feed to carry ServeCalendarFeed's own Content-Type, got %q", contentType)
				}
				assertNoSetCookie(t, response, "its HEAD")
			},
		},
		{
			name:       "api route without a session",
			path:       "/api/v1/users/current",
			accept:     "application/json",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			getResponse := headDispatchResponse(t, app, http.MethodGet, testCase.path, testCase.accept)
			if getResponse.StatusCode != testCase.wantStatus {
				t.Fatalf("GET %s answered %d, want %d — fix the probe before reading the HEAD assertion below", testCase.path, getResponse.StatusCode, testCase.wantStatus)
			}

			headResponse := headDispatchResponse(t, app, http.MethodHead, testCase.path, testCase.accept)
			if headResponse.StatusCode != testCase.wantStatus {
				t.Fatalf("HEAD %s answered %d, want %d (GET's own answer): the request reached the NotFound catch-all instead of the route's handler chain", testCase.path, headResponse.StatusCode, testCase.wantStatus)
			}
			if body := mustReadAll(t, headResponse); len(body) != 0 {
				t.Fatalf("HEAD %s returned a %d-byte body; a HEAD response carries headers only", testCase.path, len(body))
			}
			if testCase.assertHEAD != nil {
				testCase.assertHEAD(t, headResponse)
			}
		})
	}

	t.Run("unknown path", func(t *testing.T) {
		getResponse := headDispatchResponse(t, app, http.MethodGet, "/does-not-exist", "")
		if getResponse.StatusCode != http.StatusNotFound {
			t.Fatalf("GET /does-not-exist answered %d, want 404 — fix the probe before reading the HEAD assertion below", getResponse.StatusCode)
		}

		headResponse := headDispatchResponse(t, app, http.MethodHead, "/does-not-exist", "")
		if headResponse.StatusCode != http.StatusNotFound {
			t.Fatalf("HEAD /does-not-exist answered %d, want 404: an unknown path is answered by the same catch-all on either method", headResponse.StatusCode)
		}
		if got, want := headResponse.Header.Get("Content-Type"), getResponse.Header.Get("Content-Type"); got != want {
			t.Fatalf("HEAD /does-not-exist answered Content-Type %q, want GET's own %q", got, want)
		}
	})
}

// TestEveryParameterlessGETRouteAnswersHEADLikeGET is the class sweep behind
// the cases above: it walks the production route table and holds every GET
// route that needs no path parameter to the same answer on HEAD. There is no
// allowlist — a route added later is covered the day it is registered — and the
// only exemption is derived from the chain fiber will dispatch, so a route
// cannot leave the sweep without carrying the refusal middleware that puts it
// in the other branch.
func TestEveryParameterlessGETRouteAnswersHEADLikeGET(t *testing.T) {
	app, _ := newHeadDispatchTestApp(t)

	mirrored := 0
	answered := 0
	refused := []string{}
	for _, route := range app.GetRoutes(true) {
		if route.Method != fiber.MethodGet || strings.ContainsAny(route.Path, ":*+") {
			continue
		}

		t.Run(route.Path, func(t *testing.T) {
			getResponse := headDispatchResponse(t, app, http.MethodGet, route.Path, "")
			headResponse := headDispatchResponse(t, app, http.MethodHead, route.Path, "")

			if chainRefusesHEAD(route) {
				if getResponse.StatusCode == http.StatusNotFound {
					t.Fatalf("GET %s answered 404, so the HEAD assertion below cannot tell a refusal from a route that does not answer at all", route.Path)
				}
				if headResponse.StatusCode != http.StatusNotFound {
					t.Fatalf("HEAD %s answered %d, want 404: a route whose GET spends a one-time reveal must not serve a HEAD twin that spends it for a discarded body", route.Path, headResponse.StatusCode)
				}
				return
			}

			if headResponse.StatusCode != getResponse.StatusCode {
				t.Fatalf("HEAD %s answered %d while GET answered %d: a HEAD request must reach the same handler chain as GET", route.Path, headResponse.StatusCode, getResponse.StatusCode)
			}
		})

		if chainRefusesHEAD(route) {
			refused = append(refused, route.Path)
			continue
		}
		mirrored++
		if headDispatchResponse(t, app, http.MethodGet, route.Path, "").StatusCode != http.StatusNotFound {
			answered++
		}
	}

	// Three anchors, because each failure mode is silent on its own: a route
	// discovery that finds nothing, a table where every GET answers 404 (then
	// the catch-all would satisfy the comparison), and a marker that matches no
	// route (then the refusal branch above is never exercised and a shown-once
	// surface could be mirrored without anybody noticing).
	if mirrored == 0 {
		t.Fatal("no parameterless GET route was compared; recheck route discovery")
	}
	if answered == 0 {
		t.Fatal("every compared GET answered 404; the comparison would be satisfied by the catch-all alone and proves nothing")
	}
	if len(refused) == 0 {
		t.Fatalf("no route in the table chains %s; the shown-once branch above asserted nothing — recheck the middleware's name", headRefusalMarker)
	}
}

// chainRefusesHEAD reports whether the route's own chain carries the
// shown-once HEAD refusal, read off the handler fiber will dispatch rather
// than from a list kept beside it.
func chainRefusesHEAD(route fiber.Route) bool {
	for _, name := range routeHandlerNames(route) {
		if strings.Contains(name, headRefusalMarker) {
			return true
		}
	}
	return false
}

func headDispatchResponse(t *testing.T, app *fiber.App, method string, path string, accept string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(method, path, nil)
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}
