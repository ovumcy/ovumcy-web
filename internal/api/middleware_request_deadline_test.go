package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// TestRequestDeadlineGuardThreadsADeadlineToTheHandler proves the guard does
// the thing the finding was about: a deadline that actually reaches the ctx a
// handler passes down to a repository. Asserting only that the middleware ran
// would pass just as well against c.Context()'s default, which is
// context.Background() — no deadline, no cancellation, and an unbounded wait
// for a database connection underneath it.
func TestRequestDeadlineGuardThreadsADeadlineToTheHandler(t *testing.T) {
	app := fiber.New()
	app.Use(RequestDeadlineGuard(RequestBudget))

	var (
		sawDeadline bool
		remaining   time.Duration
	)
	app.Get("/probe", func(c fiber.Ctx) error {
		deadline, ok := c.Context().Deadline()
		sawDeadline = ok
		if ok {
			remaining = time.Until(deadline)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/probe", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /probe: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if !sawDeadline {
		t.Fatal("handler context carried no deadline — an unbounded request path is the defect this guard exists for")
	}
	if remaining <= 0 || remaining > RequestBudget {
		t.Fatalf("remaining budget = %s, want a positive value no greater than %s", remaining, RequestBudget)
	}
}

// repositoryContextObservation records what one repository-level query saw of
// the request context it was handed.
type repositoryContextObservation struct {
	table       string
	hasDeadline bool
	remaining   time.Duration
}

// observeRepositoryRequestContexts taps the persistence layer of a real test app
// and records the context every query runs under. It hooks GORM's query callback
// rather than a stub repository on purpose: the claim under test is about the
// context that reaches `db.WithContext(ctx)` at the bottom of the chain, and a
// substituted repository would be observing the test's own wiring instead of the
// app's.
func observeRepositoryRequestContexts(t *testing.T, database *gorm.DB) func() []repositoryContextObservation {
	t.Helper()

	const callbackName = "ovumcy:test:observe_request_deadline"

	var (
		mutex        sync.Mutex
		observations []repositoryContextObservation
	)
	if err := database.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		observation := repositoryContextObservation{table: tx.Statement.Table}
		if tx.Statement.Context != nil {
			if deadline, ok := tx.Statement.Context.Deadline(); ok {
				observation.hasDeadline = true
				observation.remaining = time.Until(deadline)
			}
		}
		mutex.Lock()
		observations = append(observations, observation)
		mutex.Unlock()
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Callback().Query().Remove(callbackName)
	})

	return func() []repositoryContextObservation {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]repositoryContextObservation(nil), observations...)
	}
}

// TestRegisterRoutesMountsTheDeadlineGuardDownToTheRepository pins the guard's
// INSTALLATION, which nothing did. The two tests above build their own
// fiber.New() and call app.Use themselves, so they pin how the middleware
// behaves once mounted and pass unchanged against an app that never mounts it —
// deleting the app.Use line from RegisterRoutes left ./internal/api and
// ./cmd/ovumcy entirely green. That is the finding's own argument one floor up:
// asserting the middleware works proves nothing about it being switched on.
//
// It also closes the second half of the same gap. The behaviour tests read
// c.Context() inside a hand-written handler, which proves the deadline reached
// the top of the chain and nothing about the bottom; here the observation is
// taken inside the real repository call, where a missing deadline is what lets
// database/sql wait for a connection forever. The route is an ordinary
// unauthenticated page whose handler threads c.Context() into a service and on
// into a repository, so no session or fixture is needed.
//
// Modelled on TestRequestBodyLimitGuardCoversEveryRegisteredRouteThatCanCarryAReadBody,
// the sibling registered one line below the guard: drive the app the composition
// root actually assembles, not a hand-built stand-in.
func TestRegisterRoutesMountsTheDeadlineGuardDownToTheRepository(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	readObservations := observeRepositoryRequestContexts(t, database)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/register", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /register: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	observations := readObservations()
	// The positive anchor: without a query having run, "every query carried a
	// deadline" is vacuously true and would stay green with the guard gone.
	if len(observations) == 0 {
		t.Fatal("no repository query ran during the request, so the deadline could not be observed where it matters; pick a route that reads through a repository")
	}
	for _, observation := range observations {
		if !observation.hasDeadline {
			t.Fatalf("query on %q ran under a context with no deadline: RegisterRoutes did not mount RequestDeadlineGuard, so c.Context() is still fiber's context.Background() and database/sql will wait for a connection forever", observation.table)
		}
		if observation.remaining <= 0 || observation.remaining > RequestBudget {
			t.Fatalf("query on %q had %s of budget left, want a positive window no greater than %s: the deadline it carries is not the request budget", observation.table, observation.remaining, RequestBudget)
		}
	}
}

// TestRequestDeadlineGuardRefusesToBeUnbounded pins the fallback on the seam
// that exists for tests: the budget is a parameter, so a caller could pass a
// non-positive one and quietly remove the bound this guard exists to impose.
// A miswired caller gets the constant instead.
func TestRequestDeadlineGuardRefusesToBeUnbounded(t *testing.T) {
	for _, budget := range []time.Duration{0, -time.Second} {
		app := fiber.New()
		app.Use(RequestDeadlineGuard(budget))

		var remaining time.Duration
		hadDeadline := false
		app.Get("/probe", func(c fiber.Ctx) error {
			deadline, ok := c.Context().Deadline()
			hadDeadline = ok
			if ok {
				remaining = time.Until(deadline)
			}
			return c.SendStatus(fiber.StatusNoContent)
		})

		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/probe", nil), testConfigNoTimeout)
		if err != nil {
			t.Fatalf("GET /probe with budget %s: %v", budget, err)
		}
		_ = resp.Body.Close()

		if !hadDeadline {
			t.Fatalf("budget %s left the request unbounded", budget)
		}
		if remaining <= 0 || remaining > RequestBudget {
			t.Fatalf("budget %s produced a remaining window of %s, want the %s fallback", budget, remaining, RequestBudget)
		}
	}
}

// TestRequestDeadlineGuardAnswersAnExpiredBudgetAsMappedUnavailable pins the
// answer, not just the expiry. The condition surfaces deep in a repository call
// as an opaque driver error, so without a single decision point each domain
// would map it to its own internal 500 ("failed to update day"); the client
// gets one stable key instead, and a status that says "retry", not "we are
// broken".
func TestRequestDeadlineGuardAnswersAnExpiredBudgetAsMappedUnavailable(t *testing.T) {
	app := fiber.New()
	app.Use(RequestDeadlineGuard(time.Millisecond))
	app.Get("/slow", func(c fiber.Ctx) error {
		// Stand in for a repository call that outlives the budget: block on the
		// request context exactly as database/sql does while waiting for a free
		// connection, rather than sleeping past a wall-clock figure. The
		// handler then returns normally, which is the case that matters — the
		// guard must answer on the expired budget even when the work below it
		// reports success.
		<-c.Context().Done()
		return c.SendString("handler finished anyway")
	})

	request := httptest.NewRequest(http.MethodGet, "/slow", nil)
	request.Header.Set("Accept", "application/json")
	resp, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /slow: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	body := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	detail, _ := body["error_detail"].(map[string]any)
	if detail == nil {
		t.Fatalf("expected the shared error envelope, got %v", body)
	}
	if key, _ := detail["key"].(string); key != "request_timeout" {
		t.Fatalf("error_detail.key = %q, want %q", key, "request_timeout")
	}
}

// TestRequestDeadlineGuardDiscardsTheHandlerResponseOnAnExpiredBudget covers the
// half of "answering" that is not the status line. The guard replaced the status
// and the body of a request that outlived its budget and left the handler's
// RESPONSE HEADERS in place, so a sign-in whose work had already committed
// answered 503 request_timeout while still delivering Set-Cookie and Location.
// request_timeout tells the client to retry, i.e. that nothing happened; a
// session cookie riding along says the opposite, and the client believes the
// status.
//
// The rollback is defined by when a header was set, not by its name, so the test
// asserts both directions in one run: the middleware's own headers and cookie —
// set before the guard, exactly as securityHeadersMiddleware and the CSRF
// middleware are in the composition root — must survive, while everything the
// handler set must not. Without the surviving pair the test would pass just as
// well against a guard that wiped the whole response, which is a different
// defect.
func TestRequestDeadlineGuardDiscardsTheHandlerResponseOnAnExpiredBudget(t *testing.T) {
	const (
		policyHeader  = "Content-Security-Policy"
		policyValue   = "default-src 'self'"
		handlerHeader = "X-Ovumcy-Handler-Marker"
	)

	app := fiber.New()
	// Stands in for the middleware the composition root registers ahead of the
	// routes: what it sets describes the response the guard itself emits.
	app.Use(func(c fiber.Ctx) error {
		c.Set(policyHeader, policyValue)
		c.Cookie(&fiber.Cookie{Name: "ovumcy_csrf", Value: "middleware-issued"})
		return c.Next()
	})
	app.Use(RequestDeadlineGuard(time.Millisecond))
	app.Get("/slow", func(c fiber.Ctx) error {
		// A handler whose work has already committed — the session is issued and
		// the post-sign-in redirect prepared — and only then does the budget run
		// out underneath it, exactly as it would inside a repository call.
		c.Cookie(&fiber.Cookie{Name: "ovumcy_auth", Value: "committed-session"})
		c.Set(fiber.HeaderLocation, "/dashboard")
		c.Set(handlerHeader, "handler-set")
		<-c.Context().Done()
		return c.SendString("handler finished anyway")
	})

	request := httptest.NewRequest(http.MethodGet, "/slow", nil)
	request.Header.Set("Accept", "application/json")
	resp, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /slow: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}

	cookies := map[string]string{}
	for _, cookie := range resp.Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	if value, ok := cookies["ovumcy_auth"]; ok {
		t.Fatalf("the 503 delivered the handler's session cookie ovumcy_auth=%q: a caller told to retry already holds the session the status says it never got", value)
	}
	if location := resp.Header.Get(fiber.HeaderLocation); location != "" {
		t.Fatalf("the 503 carried the handler's Location %q", location)
	}
	if marker := resp.Header.Get(handlerHeader); marker != "" {
		t.Fatalf("the 503 carried the handler's %s: %q — the rollback covers every header the handler set, not a two-name denylist", handlerHeader, marker)
	}

	if got := resp.Header.Get(policyHeader); got != policyValue {
		t.Fatalf("%s = %q, want %q — the rollback stops at the guard and must not take the app's own response headers with it", policyHeader, got, policyValue)
	}
	if value := cookies["ovumcy_csrf"]; value != "middleware-issued" {
		t.Fatalf("ovumcy_csrf = %q, want the value the middleware issued before the guard ran; dropping every Set-Cookie is not the invariant", value)
	}

	body := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	detail, _ := body["error_detail"].(map[string]any)
	if key, _ := detail["key"].(string); key != "request_timeout" {
		t.Fatalf("error_detail.key = %q, want %q", key, "request_timeout")
	}
}
