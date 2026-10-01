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

	app := fiber.New()
	app.Use(MethodOverride(newBareRefusalHandler(t)))
	app.All("/*", func(c fiber.Ctx) error {
		back, ok := plainPageFormBackPath(c)
		return c.SendString(strconv.FormatBool(ok) + " " + back)
	})

	const browser = "text/html,application/xhtml+xml"
	const calendar = "true /calendar?month=2026-09&day=2026-09-27"
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
		{name: "reminders form is another route", method: http.MethodPost, target: "/api/v1/users/current/reminders", accept: browser, body: "_method=PATCH", want: "false "},
		{name: "other api route", method: http.MethodPost, target: "/api/v1/symptoms", accept: browser, want: "false "},
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
