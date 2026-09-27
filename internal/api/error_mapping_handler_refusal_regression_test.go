package api

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// newBareRefusalHandler builds a real handler with empty template registries,
// so a route can reach render/renderPartial/OwnerOnly refusals that the
// production template set never produces.
func newBareRefusalHandler(t *testing.T) *Handler {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "refusal.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	handler, err := NewHandler(testAppSecretKey, time.UTC, i18nManager, false, newTestHandlerDependencies(database, i18nManager))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}
	handler.templates = map[string]*template.Template{
		// "base" fails at execution time: index out of range on an empty slice.
		"broken": template.Must(template.New("base").Parse(`{{index .Items 3}}`)),
	}
	handler.partials = map[string]*template.Template{
		"broken": template.Must(template.New("broken").Parse(`{{index .Items 3}}`)),
	}
	return handler
}

// TestHandlerRefusalsGoThroughTheHandlerErrorResponder pins the sites that
// answer a refusal from a handler-owned path (render, renderPartial,
// OwnerOnly): each must answer with its own spec's status and the stable JSON
// key, through the handler's responder rather than the package-level one.
func TestHandlerRefusalsGoThroughTheHandlerErrorResponder(t *testing.T) {
	t.Parallel()

	handler := newBareRefusalHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)

	app.Get("/render-missing", func(c fiber.Ctx) error { return handler.render(c, "missing", fiber.Map{}) })
	app.Get("/render-broken", func(c fiber.Ctx) error { return handler.render(c, "broken", fiber.Map{"Items": []int{}}) })
	app.Get("/partial-missing", func(c fiber.Ctx) error { return handler.renderPartial(c, "missing", fiber.Map{}) })
	app.Get("/owner-anonymous", handler.OwnerOnly, func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	app.Get("/owner-partner", func(c fiber.Ctx) error {
		c.Locals(contextUserKey, &models.User{Role: "partner"})
		return c.Next()
	}, handler.OwnerOnly, func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	cases := []struct {
		path string
		want APIErrorSpec
	}{
		{"/render-missing", templateNotFoundErrorSpec()},
		{"/render-broken", templateRenderErrorSpec()},
		{"/partial-missing", partialRenderErrorSpec()},
		{"/owner-anonymous", unauthorizedErrorSpec()},
		{"/owner-partner", ownerAccessRequiredErrorSpec()},
	}
	for _, tc := range cases {
		request := httptest.NewRequest(http.MethodGet, tc.path, nil)
		request.Header.Set("Accept", "application/json")
		response, err := app.Test(request)
		if err != nil {
			t.Fatalf("%s: request failed: %v", tc.path, err)
		}
		if response.StatusCode != tc.want.Status {
			t.Fatalf("%s: status got %d want %d", tc.path, response.StatusCode, tc.want.Status)
		}
		if body := mustReadBodyString(t, response.Body); !strings.Contains(body, `"error":"`+tc.want.Key+`"`) {
			t.Fatalf("%s: expected error key %q in %q", tc.path, tc.want.Key, body)
		}
		_ = response.Body.Close()
	}
}
