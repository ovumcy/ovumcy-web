package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestSafeRequestLogPathMasksTheFeedTokenWhenNoRouteMatched drives
// SafeRequestLogPath itself on a request no route owns: the only thing the
// context offers is a USE route, so the function falls back to the raw path and
// the mask on the "<token>.ics" segment is the only thing between the token and
// the log. A request that hits the registered feed route never reaches that
// branch, which is why the route-template tests cannot see it.
func TestSafeRequestLogPathMasksTheFeedTokenWhenNoRouteMatched(t *testing.T) {
	const rawToken = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789ABCDEFGHJKLMNP"
	for _, suffix := range []string{".ics", ".ICS", ".Ics"} {
		var logged string
		// No route at all: the router answers 404 itself and the error handler
		// sees a context whose matched route carries no path.
		app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
			logged = SafeRequestLogPath(c)
			return c.SendStatus(http.StatusNotFound)
		}})
		response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/calendar/feed/"+rawToken+suffix, nil))
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("suffix %q: the unrouted request answered %d, want the 404 that proves no feed route matched", suffix, response.StatusCode)
		}
		if strings.Contains(logged, rawToken) {
			t.Fatalf("suffix %q: the unrouted fallback logged the token: %q", suffix, logged)
		}
		if logged != "/calendar/feed/:token.ics" {
			t.Fatalf("suffix %q: the unrouted fallback logged %q, want /calendar/feed/:token.ics", suffix, logged)
		}
	}
}
