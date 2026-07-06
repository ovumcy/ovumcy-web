package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
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
		app.Use(func(c fiber.Ctx) error {
			c.Locals(contextUserKey, &models.User{ID: 1, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5})
			return c.Next()
		})
	}

	app.Get("/api/days", handler.GetDays)
	app.Get("/api/days/:date", handler.GetDay)
	app.Get("/api/v1/symptoms", handler.GetSymptoms)
	app.Get("/api/v1/exports/csv", handler.ExportCSV)
	app.Get("/api/v1/exports/json", handler.ExportJSON)
	app.Put("/api/days/:date", handler.UpsertDay)
	app.Delete("/api/days/:date", handler.DeleteDay)
	app.Post("/api/days/:date/cycle-start", handler.MarkCycleStart)
	app.Post("/api/v1/symptoms", handler.CreateSymptom)
	app.Patch("/api/v1/symptoms/:id", handler.UpdateSymptom)
	app.Delete("/api/v1/symptoms/:id", handler.DeleteSymptom)
	app.Post("/api/v1/symptoms/:id/restore", handler.RestoreSymptom)
	app.Patch("/api/v1/users/current/tracking", handler.UpdateTrackingSettings)
	app.Post("/api/v1/users/current/timezone", handler.UpdateTimezone)
	app.Patch("/api/v1/users/current/cycle", handler.UpdateCycleSettings)
	app.Patch("/api/v1/users/current/reminders", handler.UpdateReminderSettings)
	app.Post("/api/v1/users/current/webhook", handler.UpdateWebhookSettings)
	app.Post("/api/v1/users/current/calendar-feed", handler.GenerateCalendarFeed)
	app.Post("/api/v1/users/current/calendar-feed/rotate", handler.RotateCalendarFeed)
	app.Delete("/api/v1/users/current/calendar-feed", handler.RevokeCalendarFeed)
	app.Post("/api/v1/onboarding/complete", handler.OnboardingComplete)
	app.Get("/onboarding", handler.ShowOnboarding)
	app.Get("/settings/2fa", handler.ShowTOTPSetupPage)
	app.Get("/settings/calendar-feed", handler.ShowCalendarFeedRevealPage)
	return app, database
}

// Full-page handlers guard currentUser with a redirect rather than a 401 —
// same defense-in-depth contract, HTML-shaped response.
func TestPageHandlersRedirectMissingUserAtHandlerLevel(t *testing.T) {
	app, _ := newMutationBranchTestApp(t, false)

	for _, path := range []string{"/onboarding", "/settings/2fa", "/settings/calendar-feed"} {
		response := mutationBranchRequest(t, app, http.MethodGet, path, "", "")
		if response.StatusCode != http.StatusSeeOther {
			t.Errorf("GET %s without user: expected 303, got %d", path, response.StatusCode)
		}
		if location := response.Header.Get("Location"); location != "/login" {
			t.Errorf("GET %s without user: expected redirect to /login, got %q", path, location)
		}
		_ = response.Body.Close()
	}
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

	response, err := app.Test(request, testConfigNoTimeout)
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
		{http.MethodPut, "/api/days/2026-02-17"},
		{http.MethodDelete, "/api/days/2026-02-17"},
		{http.MethodPost, "/api/days/2026-02-17/cycle-start"},
		{http.MethodPost, "/api/v1/symptoms"},
		{http.MethodPatch, "/api/v1/symptoms/1"},
		{http.MethodDelete, "/api/v1/symptoms/1"},
		{http.MethodPost, "/api/v1/symptoms/1/restore"},
		{http.MethodPatch, "/api/v1/users/current/tracking"},
		{http.MethodPost, "/api/v1/users/current/timezone"},
		{http.MethodPatch, "/api/v1/users/current/cycle"},
		{http.MethodPatch, "/api/v1/users/current/reminders"},
		{http.MethodPost, "/api/v1/users/current/webhook"},
		{http.MethodPost, "/api/v1/users/current/calendar-feed"},
		{http.MethodPost, "/api/v1/users/current/calendar-feed/rotate"},
		{http.MethodDelete, "/api/v1/users/current/calendar-feed"},
	}
	for _, testCase := range cases {
		response := mutationBranchRequest(t, app, testCase.method, testCase.path, "", "")
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without user: expected 401, got %d", testCase.method, testCase.path, response.StatusCode)
		}
		_ = response.Body.Close()
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
		{"day delete invalid date", http.MethodDelete, "/api/days/garbage", "", ""},
		{"cycle start invalid date", http.MethodPost, "/api/days/garbage/cycle-start", "", ""},
		{"symptom create empty name", http.MethodPost, "/api/v1/symptoms", url.Values{"name": {"   "}}.Encode(), form},
		{"symptom create malformed json", http.MethodPost, "/api/v1/symptoms", "{", "application/json"},
		{"symptom update malformed json", http.MethodPatch, "/api/v1/symptoms/1", "{", "application/json"},
		{"symptom update invalid id", http.MethodPatch, "/api/v1/symptoms/garbage", url.Values{"name": {"Cramps"}}.Encode(), form},
		{"symptom update empty name", http.MethodPatch, "/api/v1/symptoms/1", url.Values{"name": {"   "}}.Encode(), form},
		{"symptom restore invalid id", http.MethodPost, "/api/v1/symptoms/garbage/restore", "", ""},
		{"tracking malformed json", http.MethodPatch, "/api/v1/users/current/tracking", "{", "application/json"},
		{"timezone malformed json", http.MethodPost, "/api/v1/users/current/timezone", "{", "application/json"},
		{"timezone unsafe value", http.MethodPost, "/api/v1/users/current/timezone", url.Values{"timezone": {"Local"}}.Encode(), form},
		{"cycle length out of range", http.MethodPatch, "/api/v1/users/current/cycle", url.Values{"cycle_length": {"999"}, "period_length": {"5"}}.Encode(), form},
		{"reminders malformed json", http.MethodPatch, "/api/v1/users/current/reminders", "{", "application/json"},
		{"reminders non-integer form value", http.MethodPatch, "/api/v1/users/current/reminders", url.Values{"reminder_lead_days": {"soon"}}.Encode(), form},
		{"webhook malformed json", http.MethodPost, "/api/v1/users/current/webhook", "{", "application/json"},
		{"onboarding complete steps required", http.MethodPost, "/api/v1/onboarding/complete", "", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := mutationBranchRequest(t, app, testCase.method, testCase.path, testCase.body, testCase.contentType)
			defer func() { _ = response.Body.Close() }()
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
		{"day delete by path", http.MethodDelete, "/api/days/2026-02-17", "", ""},
		{"day upsert", http.MethodPut, "/api/days/2026-02-17", url.Values{"is_period": {"true"}, "flow": {"medium"}}.Encode(), form},
		{"cycle start mark", http.MethodPost, "/api/days/2026-02-17/cycle-start", "", ""},
		{"cycle settings save", http.MethodPatch, "/api/v1/users/current/cycle", url.Values{"cycle_length": {"28"}, "period_length": {"5"}}.Encode(), form},
		{"tracking settings save", http.MethodPatch, "/api/v1/users/current/tracking", url.Values{"track_bbt": {"true"}}.Encode(), form},
		// A valid IANA zone that differs from the stub user's empty Timezone
		// reaches PersistTimezone -> the repo UPDATE, which fails with the DB
		// closed (settingsTimezoneUpdateErrorSpec tail).
		{"timezone settings save", http.MethodPost, "/api/v1/users/current/timezone", url.Values{"timezone": {"Europe/Belgrade"}}.Encode(), form},
		// The stub user's ReminderLeadDays is 0, so a clamped value of 7 differs
		// and reaches SaveReminderLeadDays -> the repo UPDATE, which fails with
		// the DB closed (settingsRemindersUpdateErrorSpec tail).
		{"reminder settings save", http.MethodPatch, "/api/v1/users/current/reminders", url.Values{"reminder_lead_days": {"7"}}.Encode(), form},
		// A webhook save with the DB closed fails inside
		// SaveWebhookSettingsFromForm's LoadSettingsByID read, mapped through
		// mapSettingsWebhookSaveError's default branch to the 500 tail
		// (settingsWebhookUpdateErrorSpec).
		{"webhook settings save", http.MethodPost, "/api/v1/users/current/webhook", url.Values{"webhook_enabled": {"true"}, "webhook_url": {"https://ntfy.example/down"}}.Encode(), form},
		{"day list fetch", http.MethodGet, "/api/days?from=2026-01-01&to=2026-02-01", "", ""},
		{"day fetch", http.MethodGet, "/api/days/2026-02-17", "", ""},
		{"symptom list fetch", http.MethodGet, "/api/v1/symptoms", "", ""},
		{"export csv fetch", http.MethodGet, "/api/v1/exports/csv", "", ""},
		{"export json fetch", http.MethodGet, "/api/v1/exports/json", "", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := mutationBranchRequest(t, app, testCase.method, testCase.path, testCase.body, testCase.contentType)
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode < 400 {
				t.Fatalf("expected a mapped error with the database down, got %d", response.StatusCode)
			}
		})
	}
}
