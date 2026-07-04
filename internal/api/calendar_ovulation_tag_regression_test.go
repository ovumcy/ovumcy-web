package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestCalendarRendersOvulationTagWithoutFertileOverride(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-ovulation-tag@example.com", "StrongPass1", true)
	periodStart := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": periodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}

	for offset := 0; offset < 5; offset++ {
		if err := database.Create(&models.DailyLog{
			UserID:   user.ID,
			Date:     periodStart.AddDate(0, 0, offset),
			IsPeriod: true,
			Flow:     models.FlowMedium,
		}).Error; err != nil {
			t.Fatalf("create period log day %d: %v", offset, err)
		}
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	febRendered := renderCalendarMonthHTML(t, app, authCookie, "2026-02")
	febDayMarkup := extractCalendarDayMarkup(t, febRendered, "2026-02-23")
	if !regexp.MustCompile(`calendar-ovulation-dot`).MatchString(febDayMarkup) {
		t.Fatalf("expected ovulation dot on 2026-02-23 in February calendar")
	}
	if regexp.MustCompile(`calendar-tag-label-full">Ovulation</span>`).MatchString(febDayMarkup) {
		t.Fatalf("did not expect textual ovulation badge on 2026-02-23")
	}

	marRendered := renderCalendarMonthHTML(t, app, authCookie, "2026-03")
	marDayMarkup := extractCalendarDayMarkup(t, marRendered, "2026-03-23")
	if !regexp.MustCompile(`calendar-ovulation-dot`).MatchString(marDayMarkup) {
		t.Fatalf("expected projected ovulation dot on 2026-03-23 in March calendar")
	}
}

func renderCalendarMonthHTML(t *testing.T, app *fiber.App, authCookie string, month string) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/calendar?month="+month, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar request for month %s failed: %v", month, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for month %s, got %d", month, response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read calendar body for month %s: %v", month, err)
	}

	return string(body)
}

func extractCalendarDayMarkup(t *testing.T, rendered string, day string) string {
	t.Helper()

	pattern := regexp.MustCompile(`(?s)<button[^>]*data-day="` + regexp.QuoteMeta(day) + `"[^>]*>.*?</button>`)
	match := pattern.FindString(rendered)
	if match == "" {
		t.Fatalf("expected calendar markup for day %s", day)
	}
	return match
}
