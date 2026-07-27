package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
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
