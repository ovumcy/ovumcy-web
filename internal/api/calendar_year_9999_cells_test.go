package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// WEB-125: a December 9999 grid cell whose date ParseDayDate refuses is drawn
// inert — disabled, with no hx-get — so a click cannot request
// /calendar/day/10000-01-01 and fail. The grid is built for that month
// directly: the route's navigation horizon keeps it out of reach of today's
// clock.
func TestDecember9999CalendarCellsPastDayDateMaxAreInert(t *testing.T) {
	t.Parallel()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "calendar-9999.db")})
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
	user := createOnboardingTestUser(t, database, "calendar-9999@example.com", "StrongPass1", true)

	app := fiber.New()
	app.Get("/probe", func(c fiber.Ctx) error {
		now := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
		monthStart := time.Date(9999, time.December, 1, 0, 0, 0, 0, time.UTC)
		data, err := handler.buildCalendarViewData(context.Background(), &user, "en", i18nManager.Messages("en"), now, monthStart, "", time.UTC)
		if err != nil {
			return err
		}
		return handler.render(c, "calendar", data)
	})
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/probe", nil))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	html := string(body)

	for key, wantSelectable := range map[string]bool{
		"9999-12-30":  true,
		"9999-12-31":  false,
		"10000-01-01": false,
	} {
		button := regexp.MustCompile(`<button\s[^>]*data-day="` + regexp.QuoteMeta(key) + `"[^>]*>`).FindString(html)
		if button == "" {
			t.Fatalf("the December 9999 grid rendered no cell for %s", key)
		}
		hasGet := strings.Contains(button, `hx-get="/calendar/day/`+key)
		disabled := regexp.MustCompile(`\sdisabled[\s>]`).MatchString(button)
		if hasGet != wantSelectable || disabled == wantSelectable {
			t.Errorf("%s: hx-get %v, disabled %v, want selectable %v: %s", key, hasGet, disabled, wantSelectable, button)
		}
	}
	if strings.Contains(html, "/calendar/day/10000-") {
		t.Fatal("the page still names a year-10000 day URL")
	}
}
