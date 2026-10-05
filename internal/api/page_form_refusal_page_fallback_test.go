package api

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// TestPageFormRefusalPageFallsBackToTheFragment pins the refusal page's floor: a
// page that is missing or fails to render still answers the refusal — same
// status, same key, the link back — as the bare fragment, never as a template
// error mapped back into the branch that is rendering it.
func TestPageFormRefusalPageFallsBackToTheFragment(t *testing.T) {
	t.Parallel()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	broken := template.Must(template.New("base").Parse(`{{define "base"}}{{template "absent" .}}{{end}}`))
	cases := map[string]map[string]*template.Template{
		"page missing":         {},
		"page fails to render": {pageFormRefusalTemplate: broken},
	}
	for name, templates := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler := &Handler{i18n: manager, templates: templates}
			app := fiber.New()
			app.Post("/refused", func(c fiber.Ctx) error {
				return handler.sendPageFormRefusalPage(c, APIErrorSpec{Status: fiber.StatusForbidden, Key: "forbidden"}, "/settings")
			})
			response := mustAppResponse(t, app, httptest.NewRequest(http.MethodPost, "/refused", nil))
			defer func() { _ = response.Body.Close() }()
			assertStatusCode(t, response, http.StatusForbidden)
			body := mustReadBodyString(t, response.Body)
			if strings.Contains(body, "<html") || !strings.Contains(body, `data-flash-key="`) || !strings.Contains(body, `<a href="/settings"`) {
				t.Fatalf("fallback is not the bare fragment with its back link: %s", body)
			}
		})
	}
}
