package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestPlainPageFormBackPathAdmitsOnlyBrowserFormPostsItCanLinkBackFrom(t *testing.T) {
	t.Parallel()

	// The real override middleware, so a form that names PATCH or DELETE arrives
	// here the way it does in production: as that verb, flagged as a form post.
	app := fiber.New()
	app.Use(MethodOverride(newBareRefusalHandler(t)))
	app.All("/*", func(c fiber.Ctx) error {
		back, ok := plainPageFormBackPath(c)
		return c.SendString(strconv.FormatBool(ok) + " " + back)
	})

	const browser = "text/html,application/xhtml+xml"
	const calendar = "true /calendar?month=2026-09&day=2026-09-27"
	const settings = "true /settings"
	cases := []struct {
		name, method, target, accept, hx, body, want string
	}{
		{name: "onboarding step 1", method: http.MethodPost, target: "/api/v1/onboarding/steps/1", accept: browser, want: "true /onboarding?step=1"},
		{name: "onboarding step 2", method: http.MethodPost, target: "/api/v1/onboarding/steps/2", accept: browser, want: "true /onboarding?step=2"},
		{name: "dashboard cycle start", method: http.MethodPost, target: "/api/v1/days/2026-09-27/cycle-start?source=dashboard", accept: browser, want: "true /dashboard"},
		{name: "calendar cycle start", method: http.MethodPost, target: "/api/v1/days/2026-09-27/cycle-start?source=calendar", accept: browser, want: calendar},
		{name: "unparseable date falls back to the dashboard", method: http.MethodPost, target: "/api/v1/days/2026-13-45/cycle-start?source=calendar", accept: browser, want: "true /dashboard"},
		{name: "day form, dashboard default", method: http.MethodPost, target: "/api/v1/days/2026-09-27", accept: browser, want: "true /dashboard"},
		{name: "day form, calendar source", method: http.MethodPost, target: "/api/v1/days/2026-09-27?source=calendar", accept: browser, want: calendar},
		{name: "day form saved as PUT", method: http.MethodPost, target: "/api/v1/days/2026-09-27?source=calendar", accept: browser, body: "_method=PUT", want: calendar},
		{name: "day form deleted as DELETE", method: http.MethodPost, target: "/api/v1/days/2026-09-27?source=calendar", accept: browser, body: "_method=DELETE", want: calendar},
		{name: "calendar day form names its source in a field", method: http.MethodPost, target: "/api/v1/days/2026-09-27", accept: browser, body: "_method=PUT&source=calendar", want: calendar},
		{name: "odd source field never picks a page", method: http.MethodPost, target: "/api/v1/days/2026-09-27", accept: browser, body: "_method=PUT&source=https://evil.example/", want: "true /dashboard"},
		{name: "body of a form with no override is never read", method: http.MethodPost, target: "/api/v1/days/2026-09-27", accept: browser, body: "source=calendar", want: "true /dashboard"},
		{name: "dashboard day form saved as PUT", method: http.MethodPost, target: "/api/v1/days/2026-09-27", accept: browser, body: "_method=PUT", want: "true /dashboard"},
		{name: "unparseable day date falls back to the dashboard", method: http.MethodPost, target: "/api/v1/days/not-a-date?source=calendar", accept: browser, want: "true /dashboard"},
		{name: "odd source never picks a page", method: http.MethodPost, target: "/api/v1/days/2026-09-27?source=https://evil.example/", accept: browser, want: "true /dashboard"},
		{name: "day form real PUT", method: http.MethodPut, target: "/api/v1/days/2026-09-27?source=calendar", accept: browser, want: "false "},
		{name: "day form real DELETE", method: http.MethodDelete, target: "/api/v1/days/2026-09-27?source=calendar", accept: browser, want: "false "},
		{name: "day form GET", method: http.MethodGet, target: "/api/v1/days/2026-09-27", accept: browser, want: "false "},
		{name: "day route with a further segment", method: http.MethodPost, target: "/api/v1/days/2026-09-27/other", accept: browser, want: "false "},
		{name: "days collection", method: http.MethodPost, target: "/api/v1/days/", accept: browser, want: "false "},
		{name: "day form without text/html", method: http.MethodPost, target: "/api/v1/days/2026-09-27", accept: "", body: "_method=PUT", want: "false "},
		{name: "day form JSON client", method: http.MethodPost, target: "/api/v1/days/2026-09-27", accept: "application/json, text/html", body: "_method=PUT", want: "false "},
		{name: "day form htmx", method: http.MethodPost, target: "/api/v1/days/2026-09-27", accept: browser, hx: "true", body: "_method=PUT", want: "false "},
		{name: "cycle form, settings default", method: http.MethodPost, target: "/api/v1/users/current/cycle", accept: browser, body: "_method=PATCH", want: "true /settings"},
		{name: "cycle form, dashboard source", method: http.MethodPost, target: "/api/v1/users/current/cycle?source=dashboard", accept: browser, body: "_method=PATCH", want: "true /dashboard"},
		{name: "cycle form, odd source", method: http.MethodPost, target: "/api/v1/users/current/cycle?source=//evil.example", accept: browser, body: "_method=PATCH", want: "true /settings"},
		{name: "cycle form real PATCH", method: http.MethodPatch, target: "/api/v1/users/current/cycle?source=dashboard", accept: browser, want: "false "},
		{name: "cycle form JSON client", method: http.MethodPost, target: "/api/v1/users/current/cycle", accept: "application/json", body: "_method=PATCH", want: "false "},
		{name: "symptom create", method: http.MethodPost, target: "/api/v1/symptoms", accept: browser, want: settings},
		{name: "symptom restore", method: http.MethodPost, target: "/api/v1/symptoms/7/restore", accept: browser, want: settings},
		{name: "symptom edit, PATCH by override", method: http.MethodPost, target: "/api/v1/symptoms/7", accept: browser, body: "_method=PATCH", want: settings},
		{name: "symptom hide, DELETE by override", method: http.MethodPost, target: "/api/v1/symptoms/7", accept: browser, body: "_method=DELETE", want: settings},
		{name: "reminders, PATCH by override", method: http.MethodPost, target: "/api/v1/users/current/reminders", accept: browser, body: "_method=PATCH", want: settings},
		{name: "interface, PATCH by override", method: http.MethodPost, target: "/api/v1/users/current/interface", accept: browser, body: "_method=PATCH", want: settings},
		{name: "tracking, PATCH by override", method: http.MethodPost, target: "/api/v1/users/current/tracking", accept: browser, body: "_method=PATCH", want: settings},
		{name: "symptom id never reaches the link", method: http.MethodPost, target: "/api/v1/symptoms/%22%3E%3Cb%3E", accept: browser, want: settings},
		{name: "symptom path with a second segment", method: http.MethodPost, target: "/api/v1/symptoms/7/8", accept: browser, want: "false "},
		{name: "symptom restore with no id", method: http.MethodPost, target: "/api/v1/symptoms//restore", accept: browser, want: "false "},
		{name: "PATCH sent as PATCH keeps the envelope", method: http.MethodPatch, target: "/api/v1/users/current/interface", accept: browser, want: "false "},
		{name: "DELETE sent as DELETE keeps the envelope", method: http.MethodDelete, target: "/api/v1/symptoms/7", accept: browser, want: "false "},
		{name: "override on a route with no page", method: http.MethodPost, target: "/api/v1/stats/overview", accept: browser, body: "_method=PATCH", want: "false "},
		{name: "settings override from htmx", method: http.MethodPost, target: "/api/v1/symptoms/7", accept: browser, hx: "true", body: "_method=PATCH", want: "false "},
		{name: "settings override without text/html", method: http.MethodPost, target: "/api/v1/symptoms/7", accept: "", body: "_method=PATCH", want: "false "},
		{name: "other api route", method: http.MethodPost, target: "/api/v1/stats/overview", accept: browser, want: "false "},
		{name: "profile form", method: http.MethodPost, target: "/api/v1/users/current/profile", accept: browser, body: "_method=PATCH", want: "true /settings"},
		{name: "password change form", method: http.MethodPost, target: "/api/v1/users/current/password", accept: browser, body: "_method=PUT", want: "true /settings"},
		{name: "local password step-up form", method: http.MethodPost, target: "/api/v1/users/current/password/step-up", accept: browser, want: "true /settings"},
		{name: "recovery code form", method: http.MethodPost, target: "/api/v1/users/current/recovery-code", accept: browser, want: "true /settings"},
		{name: "identity link step-up form", method: http.MethodPost, target: "/api/v1/users/current/oidc/link/step-up", accept: browser, want: "true /settings"},
		{name: "identity unlink form", method: http.MethodPost, target: "/api/v1/users/current/oidc/identities/7", accept: browser, body: "_method=DELETE", want: "true /settings"},
		{name: "identity unlink id never reaches the link", method: http.MethodPost, target: "/api/v1/users/current/oidc/identities/%3Cscript%3E?source=calendar", accept: browser, body: "_method=DELETE", want: "true /settings"},
		{name: "identity collection has no id", method: http.MethodPost, target: "/api/v1/users/current/oidc/identities/", accept: browser, body: "_method=DELETE", want: "false "},
		{name: "identity route with a further segment", method: http.MethodPost, target: "/api/v1/users/current/oidc/identities/7/other", accept: browser, body: "_method=DELETE", want: "false "},
		{name: "2fa enrol form", method: http.MethodPost, target: "/api/v1/users/current/2fa", accept: browser, body: "_method=PUT", want: "true /settings/2fa"},
		{name: "2fa disable form", method: http.MethodPost, target: "/api/v1/users/current/2fa", accept: browser, body: "_method=DELETE", want: "true /settings/2fa"},
		{name: "2fa form, odd source", method: http.MethodPost, target: "/api/v1/users/current/2fa?source=//evil.example", accept: browser, body: "_method=DELETE", want: "true /settings/2fa"},
		{name: "profile form, upper-case spelling the router accepts", method: http.MethodPost, target: "/API/v1/users/current/Profile", accept: browser, body: "_method=PATCH", want: "true /settings"},
		{name: "profile real PATCH", method: http.MethodPatch, target: "/api/v1/users/current/profile", accept: browser, want: "false "},
		{name: "2fa real DELETE", method: http.MethodDelete, target: "/api/v1/users/current/2fa", accept: browser, want: "false "},
		{name: "recovery code JSON client", method: http.MethodPost, target: "/api/v1/users/current/recovery-code", accept: "application/json", want: "false "},
		{name: "step-up without text/html", method: http.MethodPost, target: "/api/v1/users/current/password/step-up", accept: "", want: "false "},
		{name: "2fa htmx", method: http.MethodPost, target: "/api/v1/users/current/2fa", accept: browser, hx: "true", body: "_method=PUT", want: "false "},
		{name: "password route with a further segment", method: http.MethodPost, target: "/api/v1/users/current/password/extra", accept: browser, want: "false "},
		{name: "delete account", method: http.MethodPost, target: "/api/v1/users/current", accept: browser, body: "_method=DELETE", want: "true /settings"},
		{name: "delete account, hostile source never reaches the link", method: http.MethodPost, target: "/api/v1/users/current?source=https://evil.example/", accept: browser, body: "_method=DELETE", want: "true /settings"},
		{name: "account root as a plain POST", method: http.MethodPost, target: "/api/v1/users/current", accept: browser, want: "true /settings"},
		{name: "account root GET", method: http.MethodGet, target: "/api/v1/users/current", accept: browser, want: "false "},
		{name: "account root JSON client", method: http.MethodPost, target: "/api/v1/users/current", accept: "application/json", body: "_method=DELETE", want: "false "},
		{name: "account root htmx", method: http.MethodPost, target: "/api/v1/users/current", accept: browser, hx: "true", body: "_method=DELETE", want: "false "},
		{name: "data wipe", method: http.MethodPost, target: "/api/v1/users/current/data-wipe", accept: browser, want: "true /settings"},
		{name: "data wipe step-up", method: http.MethodPost, target: "/api/v1/users/current/data-wipe/step-up", accept: browser, want: "true /settings"},
		{name: "account deletion step-up", method: http.MethodPost, target: "/api/v1/users/current/deletion/step-up", accept: browser, want: "true /settings"},
		{name: "webhook save", method: http.MethodPost, target: "/api/v1/users/current/webhook", accept: browser, want: "true /settings"},
		{name: "webhook remove", method: http.MethodPost, target: "/api/v1/users/current/webhook", accept: browser, body: "_method=DELETE", want: "true /settings"},
		{name: "calendar feed generate", method: http.MethodPost, target: "/api/v1/users/current/calendar-feed", accept: browser, want: "true /settings"},
		{name: "calendar feed revoke", method: http.MethodPost, target: "/api/v1/users/current/calendar-feed", accept: browser, body: "_method=DELETE", want: "true /settings"},
		{name: "calendar feed rotate", method: http.MethodPost, target: "/api/v1/users/current/calendar-feed/rotate", accept: browser, want: "true /settings"},
		{name: "webhook real DELETE", method: http.MethodDelete, target: "/api/v1/users/current/webhook", accept: browser, want: "false "},
		{name: "data wipe password pre-check is script-only", method: http.MethodPost, target: "/api/v1/users/current/data-wipe/validate", accept: browser, want: "false "},
		{name: "timezone post is script-only", method: http.MethodPost, target: "/api/v1/users/current/timezone", accept: browser, want: "false "},
		{name: "step-up with a further segment", method: http.MethodPost, target: "/api/v1/users/current/data-wipe/step-up/extra", accept: browser, want: "false "},
		{name: "webhook with a further segment", method: http.MethodPost, target: "/api/v1/users/current/webhook/extra", accept: browser, want: "false "},
		{name: "no text/html in Accept", method: http.MethodPost, target: "/api/v1/onboarding/steps/1", accept: "", want: "false "},
		{name: "JSON client", method: http.MethodPost, target: "/api/v1/onboarding/steps/1", accept: "application/json, text/html", want: "false "},
		{name: "htmx", method: http.MethodPost, target: "/api/v1/onboarding/steps/1", accept: browser, hx: "true", want: "false "},
		{name: "not a POST", method: http.MethodGet, target: "/api/v1/onboarding/steps/1", accept: browser, want: "false "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			if tc.body != "" {
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			if tc.accept != "" {
				request.Header.Set("Accept", tc.accept)
			}
			if tc.hx != "" {
				request.Header.Set("HX-Request", tc.hx)
			}
			response, err := app.Test(request)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != tc.want {
				t.Fatalf("got %q, want %q", body, tc.want)
			}
		})
	}
}
