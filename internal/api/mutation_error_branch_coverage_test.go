package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// These tests pin the mutation handlers' guard and failure tails that the
// fully routed app cannot reach: the handler-level currentUser checks sit
// behind AuthRequired/OwnerOnly middleware (defense in depth — a future
// route wired without the middleware must still 401), and the parse/service
// error branches all flow through failMutation/logMutationError, so this is
// also the regression net for the audited-mutation helpers.

func newMutationBranchTestApp(t *testing.T, injectUser bool) (*fiber.App, *gorm.DB) {
	t.Helper()

	handler, database := newDataAccessTestHandler(t)

	app := fiber.New()
	if injectUser {
		app.Use(func(c *fiber.Ctx) error {
			c.Locals(contextUserKey, &models.User{ID: 1, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5})
			return c.Next()
		})
	}

	app.Delete("/api/days", handler.DeleteDailyLog)
	app.Put("/api/days/:date", handler.UpsertDay)
	app.Delete("/api/days/:date", handler.DeleteDay)
	app.Post("/api/days/:date/cycle-start", handler.MarkCycleStart)
	app.Post("/api/v1/symptoms", handler.CreateSymptom)
	app.Patch("/api/v1/symptoms/:id", handler.UpdateSymptom)
	app.Delete("/api/v1/symptoms/:id", handler.DeleteSymptom)
	app.Post("/api/v1/symptoms/:id/restore", handler.RestoreSymptom)
	app.Patch("/api/v1/users/current/tracking", handler.UpdateTrackingSettings)
	app.Patch("/api/v1/users/current/cycle", handler.UpdateCycleSettings)
	return app, database
}

func mutationBranchRequest(t *testing.T, app *fiber.App, method string, path string, body string, contentType string) *http.Response {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	return response
}

func TestMutationHandlersRejectMissingUserAtHandlerLevel(t *testing.T) {
	app, _ := newMutationBranchTestApp(t, false)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodDelete, "/api/days?date=2026-02-17"},
		{http.MethodPut, "/api/days/2026-02-17"},
		{http.MethodDelete, "/api/days/2026-02-17"},
		{http.MethodPost, "/api/days/2026-02-17/cycle-start"},
		{http.MethodPost, "/api/v1/symptoms"},
		{http.MethodPatch, "/api/v1/symptoms/1"},
		{http.MethodDelete, "/api/v1/symptoms/1"},
		{http.MethodPost, "/api/v1/symptoms/1/restore"},
		{http.MethodPatch, "/api/v1/users/current/tracking"},
		{http.MethodPatch, "/api/v1/users/current/cycle"},
	}
	for _, testCase := range cases {
		response := mutationBranchRequest(t, app, testCase.method, testCase.path, "", "")
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without user: expected 401, got %d", testCase.method, testCase.path, response.StatusCode)
		}
		response.Body.Close()
	}
}

func TestMutationHandlersMapInvalidInputThroughFailMutation(t *testing.T) {
	app, _ := newMutationBranchTestApp(t, true)

	form := "application/x-www-form-urlencoded"
	cases := []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
	}{
		{"day delete invalid date", http.MethodDelete, "/api/days?date=garbage", "", ""},
		{"cycle start invalid date", http.MethodPost, "/api/days/garbage/cycle-start", "", ""},
		{"symptom create empty name", http.MethodPost, "/api/v1/symptoms", url.Values{"name": {"   "}}.Encode(), form},
		{"symptom create malformed json", http.MethodPost, "/api/v1/symptoms", "{", "application/json"},
		{"symptom update malformed json", http.MethodPatch, "/api/v1/symptoms/1", "{", "application/json"},
		{"symptom update invalid id", http.MethodPatch, "/api/v1/symptoms/garbage", url.Values{"name": {"Cramps"}}.Encode(), form},
		{"symptom update empty name", http.MethodPatch, "/api/v1/symptoms/1", url.Values{"name": {"   "}}.Encode(), form},
		{"symptom restore invalid id", http.MethodPost, "/api/v1/symptoms/garbage/restore", "", ""},
		{"tracking malformed json", http.MethodPatch, "/api/v1/users/current/tracking", "{", "application/json"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := mutationBranchRequest(t, app, testCase.method, testCase.path, testCase.body, testCase.contentType)
			defer response.Body.Close()
			if response.StatusCode < 400 || response.StatusCode >= 500 {
				t.Fatalf("expected a 4xx validation error, got %d", response.StatusCode)
			}
		})
	}
}

func TestMutationHandlersMapServiceFailuresThroughFailMutation(t *testing.T) {
	app, database := newMutationBranchTestApp(t, true)

	// Closing the database forces every repository call to fail, which
	// exercises the mapDay*/settings*UpdateErrorSpec failure tails.
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("acquire sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	form := "application/x-www-form-urlencoded"
	cases := []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
	}{
		{"day delete by query", http.MethodDelete, "/api/days?date=2026-02-17", "", ""},
		{"day delete by path", http.MethodDelete, "/api/days/2026-02-17", "", ""},
		{"day upsert", http.MethodPut, "/api/days/2026-02-17", url.Values{"is_period": {"true"}, "flow": {"medium"}}.Encode(), form},
		{"cycle start mark", http.MethodPost, "/api/days/2026-02-17/cycle-start", "", ""},
		{"cycle settings save", http.MethodPatch, "/api/v1/users/current/cycle", url.Values{"cycle_length": {"28"}, "period_length": {"5"}}.Encode(), form},
		{"tracking settings save", http.MethodPatch, "/api/v1/users/current/tracking", url.Values{"track_bbt": {"true"}}.Encode(), form},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := mutationBranchRequest(t, app, testCase.method, testCase.path, testCase.body, testCase.contentType)
			defer response.Body.Close()
			if response.StatusCode < 400 {
				t.Fatalf("expected a mapped error with the database down, got %d", response.StatusCode)
			}
		})
	}
}
