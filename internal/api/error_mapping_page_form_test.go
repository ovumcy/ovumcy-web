package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestPlainPageFormBackPathAdmitsOnlyBrowserFormPostsItCanLinkBackFrom(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.All("/*", func(c fiber.Ctx) error {
		back, ok := plainPageFormBackPath(c)
		return c.SendString(strconv.FormatBool(ok) + " " + back)
	})

	const browser = "text/html,application/xhtml+xml"
	cases := []struct {
		name, method, target, accept, hx, want string
	}{
		{"onboarding step 1", http.MethodPost, "/api/v1/onboarding/steps/1", browser, "", "true /onboarding?step=1"},
		{"onboarding step 2", http.MethodPost, "/api/v1/onboarding/steps/2", browser, "", "true /onboarding?step=2"},
		{"dashboard cycle start", http.MethodPost, "/api/v1/days/2026-09-27/cycle-start?source=dashboard", browser, "", "true /dashboard"},
		{"calendar cycle start", http.MethodPost, "/api/v1/days/2026-09-27/cycle-start?source=calendar", browser, "", "true /calendar?month=2026-09&day=2026-09-27"},
		{"unparseable date falls back to the dashboard", http.MethodPost, "/api/v1/days/2026-13-45/cycle-start?source=calendar", browser, "", "true /dashboard"},
		{"other day route", http.MethodPost, "/api/v1/days/2026-09-27", browser, "", "false "},
		{"other api route", http.MethodPost, "/api/v1/symptoms", browser, "", "false "},
		{"no text/html in Accept", http.MethodPost, "/api/v1/onboarding/steps/1", "", "", "false "},
		{"JSON client", http.MethodPost, "/api/v1/onboarding/steps/1", "application/json, text/html", "", "false "},
		{"htmx", http.MethodPost, "/api/v1/onboarding/steps/1", browser, "true", "false "},
		{"not a POST", http.MethodGet, "/api/v1/onboarding/steps/1", browser, "", "false "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(tc.method, tc.target, nil)
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
