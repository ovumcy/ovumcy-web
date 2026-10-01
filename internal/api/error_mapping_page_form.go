package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
)

// plainPageFormBackPath resolves the page a browser posted one of the in-app
// hx-post forms from when it did so without JavaScript: the onboarding steps,
// the manual cycle-start control, the day editors and the cycle settings. A
// refusal there would otherwise paint the JSON envelope as the page, so
// apiError answers the localized status fragment with a link back instead —
// same status, same key. No cookie rides on it, the flash included.
//
// Scoped tighter than isPlainAuthFormPageNavigation: the request must also say
// it accepts text/html, because API clients post form bodies to these routes
// with no Accept header at all and keep the JSON envelope. A form that names a
// verb with its hidden _method field is routed as that verb before CSRF and
// the limiters run, so c.Method() is PUT, PATCH or DELETE by the time a refusal
// reaches here; arrivedAsOverriddenFormPost says that request was still a form
// POST, and a client sending the real verb is not one.
//
// The link is built from the route alone. The date is re-formatted from the
// parsed value and `source` only picks between two fixed pages, so nothing the
// request carries reaches the href verbatim.
func plainPageFormBackPath(c fiber.Ctx) (string, bool) {
	if (c.Method() != fiber.MethodPost && !arrivedAsOverriddenFormPost(c)) || responseFormat(c) != httpx.ResponseFormatHTML {
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
	case "/api/v1/users/current/cycle":
		// The same route serves the dashboard's goal switch and the settings
		// section; the dashboard form names itself, and anything else lands on
		// settings, so an odd value is never echoed.
		if c.Query("source") == "dashboard" {
			return "/dashboard", true
		}
		return "/settings", true
	}
	rest, ok := strings.CutPrefix(path, "/api/v1/days/")
	if !ok {
		return "", false
	}
	if date, ok := strings.CutSuffix(rest, "/cycle-start"); ok {
		return dayFormBackPath(c, date), true
	}
	if rest != "" && !strings.Contains(rest, "/") {
		return dayFormBackPath(c, rest), true
	}
	return "", false
}

// dayFormBackPath is the page a day form was posted from: the calendar day when
// the form says source=calendar and the date parses, the dashboard otherwise.
// The day editor says it in a hidden field and the cycle-start form in its
// query; FormValue reads the query first, then the body, as the success path
// does (respondUpsertDaySuccess).
func dayFormBackPath(c fiber.Ctx, date string) string {
	day, err := time.Parse("2006-01-02", date)
	if err == nil && c.FormValue("source") == "calendar" {
		return calendarDayPath(day)
	}
	return "/dashboard"
}
