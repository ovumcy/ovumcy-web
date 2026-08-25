package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The onboarding gate compares `c.Path()` against a literal prefix
// (services.ShouldEnforceOnboardingAccess, called from AuthRequired), and
// `/api/v1/onboarding/../days/2026-03-01` reads as an onboarding path to that
// comparison, so the gate lets it through. That is safe today for one reason
// and one only: the router matches the same unnormalized string, so the request
// reaches no handler at all.
//
// internal/services pins the first half of that argument — the string function's
// answer — and nothing pinned the second, which is the half that lives outside
// this repository. A Fiber upgrade that routes on `URI().Path()` instead of
// `URI().PathOriginal()`, or a config that enables UnescapePath, collapses the
// traversal segment: the days route starts matching while the gate still reads
// the raw string and waves the request past onboarding. The services test stays
// green through all of it, because a pure string function cannot see a router.
//
// So this is the other half, asserted rather than assumed, and it fails in the
// same release the assumption does.
func TestRouterDoesNotNormalizeATraversalSegmentIntoAGatedRoute(t *testing.T) {
	t.Parallel()

	const traversal = "/api/v1/onboarding/../days/2026-03-01"

	// The gate's own reading of the path, restated here so the two halves are
	// visible together: it does NOT enforce onboarding on this path.
	if services.ShouldEnforceOnboardingAccess(traversal) {
		t.Fatal("fixture anchor: the gate is expected to read the traversal path as an onboarding path; " +
			"if it no longer does, this guard's premise is gone and the services-side row is what changed")
	}

	app := fiber.New()
	reached := ""
	app.Get("/api/v1/days/:date", func(c fiber.Ctx) error {
		reached = "days"
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/api/v1/onboarding/steps/:step", func(c fiber.Ctx) error {
		reached = "onboarding"
		return c.SendStatus(fiber.StatusOK)
	})

	response, err := app.Test(httptest.NewRequest(http.MethodGet, traversal, nil))
	if err != nil {
		t.Fatalf("app.Test(%q): %v", traversal, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if reached != "" {
		t.Fatalf("the router resolved %q to the %s handler; it now normalizes the traversal segment, "+
			"so the onboarding gate — which still compares the raw path — passes a request that reaches a gated route. "+
			"Clean the path in services.ShouldEnforceOnboardingAccess before this ships",
			traversal, reached)
	}
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected %q to match no route (404), got %d", traversal, response.StatusCode)
	}

	// Positive control: the same router does resolve the plain path, so a
	// 404 above is the traversal segment being taken literally and not a
	// fixture that registered nothing.
	plain := httptest.NewRequest(http.MethodGet, "/api/v1/days/2026-03-01", nil)
	plainResponse, err := app.Test(plain)
	if err != nil {
		t.Fatalf("app.Test(plain days path): %v", err)
	}
	defer func() {
		_ = plainResponse.Body.Close()
	}()
	if plainResponse.StatusCode != http.StatusOK || reached != "days" {
		t.Fatalf("fixture anchor: the plain days path must reach its handler, got status %d and reached %q", plainResponse.StatusCode, reached)
	}
}
