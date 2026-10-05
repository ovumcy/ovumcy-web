package api

import (
	"bytes"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
)

// plainPageFormBackPath resolves the page a browser posted one of the in-app
// hx-post forms from when it did so without JavaScript: the onboarding steps,
// the manual cycle-start control, the day editors, the cycle settings and the
// settings forms for reminders, interface, tracking, symptoms, the account,
// the second factor, and the erasure and egress sections. A refusal there
// would otherwise paint the JSON envelope as the page, so apiError answers the
// localized message with a link back instead, as a page in the shared layout —
// same status, same key. No cookie rides on it, the flash included. The one refusal that is not
// answered as that page is a request with no session (isSignedOutRefusal): it
// is sent to /login with the sign-in notice in the flash.
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
	if back, ok := settingsFormBackPath(path); ok {
		return back, true
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

// pageFormRefusalTemplate is the page sendPageFormRefusalPage renders into the
// shared layout.
const pageFormRefusalTemplate = "page_form_refusal"

// sendPageFormRefusalPage answers a page-form refusal as a full page: the shared
// layout, with <html lang> set to the request's language, around the localized
// message, its stable key and the link back to `back`. Status and key come from
// spec, exactly as on the bare fragment it replaces.
//
// The page does not need a session and shows nothing of one: it renders with no
// CurrentUser, so the layout draws its signed-out header — no navigation, no
// account chip, no timezone script — whichever request reached here. The CSRF
// token the layout reads is the one the middleware already put in the context,
// if any; rendering mints none and sets no cookie. The rendered address is the
// back path, never the request's own: the request's path is the API route, which
// the language switch would send the browser to and the privacy link carry.
//
// Should the page fail to render, the bare fragment carries the same refusal
// rather than a template error mapped back into this branch.
func (handler *Handler) sendPageFormRefusalPage(c fiber.Ctx, spec APIErrorSpec, back string) error {
	tmpl, ok := handler.templates[pageFormRefusalTemplate]
	if !ok {
		return sendStatusFragmentWithBackLink(c, spec, back)
	}
	message, flashKey := localizedStatusError(c, spec)
	payload := handler.withTemplateDefaults(c, fiber.Map{
		"Title":          localizedPageTitle(currentMessages(c), "app.name", "Ovumcy"),
		"CurrentPath":    back,
		"RefusalMessage": message,
		"RefusalKey":     flashKey,
		"BackPath":       back,
	})
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "base", payload); err != nil {
		return sendStatusFragmentWithBackLink(c, spec, back)
	}
	c.Status(spec.Status)
	c.Type("html", "utf-8")
	return c.Send(output.Bytes())
}

// dayFormBackPath is the page a day form was posted from: the calendar day when
// the form says source=calendar and the date parses, the dashboard otherwise.
// The cycle-start and delete forms say it in the query, the day editor's save
// in a hidden field.
func dayFormBackPath(c fiber.Ctx, date string) string {
	day, err := time.Parse("2006-01-02", date)
	if err == nil && dayFormSource(c) == "calendar" {
		return calendarDayPath(day)
	}
	return "/dashboard"
}

// dayFormSource reads the query, then the body field only of a form
// MethodOverride has already parsed into PostArgs (urlencoded, no
// Content-Encoding). A limiter refusal reaches here ahead of CSRF and the body
// cap, so no other body — multipart, compressed — is parsed for a link.
func dayFormSource(c fiber.Ctx) string {
	if source := c.Query("source"); source != "" || !arrivedAsOverriddenFormPost(c) {
		return source
	}
	return string(c.Request().PostArgs().Peek("source"))
}

// settingsFormBackPath is the one place that maps a settings-page form route
// onto the page that hosts it: the reminders, interface, tracking, symptoms,
// profile, password, recovery-code and SSO link or unlink forms, the erasure
// forms (delete account, clear data and their step-ups) and the egress forms
// (webhook, calendar feed) live on /settings, the two-factor enrol and disable
// forms on /settings/2fa. The back link is one fixed path per route. A symptom
// or identity id is matched as one path segment and never reaches the href. The
// account root is matched exactly, never as a prefix: every route under it
// would otherwise answer a page. A form on a settings page that needs a
// page-shaped refusal gets its case here.
func settingsFormBackPath(path string) (string, bool) {
	switch path {
	case "/api/v1/users/current", // DELETE: delete the account. Exact, never a prefix.
		"/api/v1/users/current/data-wipe",
		"/api/v1/users/current/data-wipe/step-up",
		"/api/v1/users/current/deletion/step-up",
		"/api/v1/users/current/webhook",
		"/api/v1/users/current/calendar-feed",
		"/api/v1/users/current/calendar-feed/rotate",
		"/api/v1/users/current/reminders",
		"/api/v1/users/current/interface",
		"/api/v1/users/current/tracking",
		"/api/v1/symptoms",
		"/api/v1/users/current/profile",
		"/api/v1/users/current/password",
		"/api/v1/users/current/password/step-up",
		"/api/v1/users/current/recovery-code",
		"/api/v1/users/current/oidc/link/step-up":
		return "/settings", true
	case "/api/v1/users/current/2fa":
		// Enrolling and disabling both live on the second-factor page.
		return "/settings/2fa", true
	}
	if id, ok := strings.CutPrefix(path, "/api/v1/users/current/oidc/identities/"); ok {
		// The unlink form of one linked identity: every identity's form sits on
		// the same page. A bare collection path never gets here: the routing
		// normalization trims its trailing slash, so the prefix no longer matches.
		if id == "" || strings.Contains(id, "/") {
			return "", false
		}
		return "/settings", true
	}
	rest, ok := strings.CutPrefix(path, "/api/v1/symptoms/")
	if !ok {
		return "", false
	}
	id, _ := strings.CutSuffix(rest, "/restore")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return "/settings", true
}
