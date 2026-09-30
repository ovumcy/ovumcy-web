package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
)

// plainPageFormBackPath resolves the page a browser posted one of the in-app
// hx-post forms from when it did so without JavaScript: the onboarding steps
// and the manual cycle-start control. A refusal there would otherwise paint the
// JSON envelope as the page, so apiError answers the localized status fragment
// with a link back instead — same status, same key.
//
// Scoped tighter than isPlainAuthFormPageNavigation: the request must also say
// it accepts text/html, because API clients post form bodies to these routes
// with no Accept header at all and keep the JSON envelope.
//
// The link is built from the route alone. The date is re-formatted from the
// parsed value and `source` only picks between two fixed pages, so nothing the
// request carries reaches the href verbatim.
func plainPageFormBackPath(c fiber.Ctx) (string, bool) {
	if c.Method() != fiber.MethodPost || responseFormat(c) != httpx.ResponseFormatHTML {
		return "", false
	}
	if !strings.Contains(strings.ToLower(c.Get(fiber.HeaderAccept)), "text/html") {
		return "", false
	}
	path := httpx.RoutingNormalizedPath(c.Path())
	switch path {
	case "/api/v1/onboarding/steps/1":
		return "/onboarding?step=1", true
	case "/api/v1/onboarding/steps/2":
		return "/onboarding?step=2", true
	}
	rest, ok := strings.CutPrefix(path, "/api/v1/days/")
	if !ok {
		return "", false
	}
	date, ok := strings.CutSuffix(rest, "/cycle-start")
	if !ok {
		return "", false
	}
	day, err := time.Parse("2006-01-02", date)
	if err == nil && c.Query("source") == "calendar" {
		return calendarDayPath(day), true
	}
	return "/dashboard", true
}
