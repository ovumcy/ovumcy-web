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

// TestCalendarRendersOvulationTagWithoutFertileOverride pins the ovulation
// marker's rendering: a dot on the ovulation day, never the textual badge, in the
// running cycle and in the next projected one.
//
// The cycle is seeded relative to the CURRENT day (cycle day 5). A fixture pinned
// to fixed calendar dates would paint nothing at all once the clock moved a week
// past the account's reference length — the calendar withholds every projected
// marker for an overdue cycle (services.DashboardCycleOverdue) — so the marker
// contract has to be measured on an account whose cycle is actually running.
func TestCalendarRendersOvulationTagWithoutFertileOverride(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-ovulation-tag@example.com", "StrongPass1", true)
	periodStart := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -4)

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": periodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}

	for offset := range 5 {
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

	// Ovulation of the running cycle: cycle start + (28 - 14) - 1. The next
	// projected cycle repeats it one cycle length later.
	currentOvulation := periodStart.AddDate(0, 0, 13)
	projectedOvulation := currentOvulation.AddDate(0, 0, 28)

	currentRendered := renderCalendarMonthHTML(t, app, authCookie, currentOvulation.Format("2006-01"))
	currentDayMarkup := extractCalendarDayMarkup(t, currentRendered, currentOvulation.Format("2006-01-02"))
	if !regexp.MustCompile(`calendar-ovulation-dot`).MatchString(currentDayMarkup) {
		t.Fatalf("expected ovulation dot on %s", currentOvulation.Format("2006-01-02"))
	}
	if regexp.MustCompile(`calendar-tag-label-full">Ovulation</span>`).MatchString(currentDayMarkup) {
		t.Fatalf("did not expect textual ovulation badge on %s", currentOvulation.Format("2006-01-02"))
	}

	projectedRendered := renderCalendarMonthHTML(t, app, authCookie, projectedOvulation.Format("2006-01"))
	projectedDayMarkup := extractCalendarDayMarkup(t, projectedRendered, projectedOvulation.Format("2006-01-02"))
	if !regexp.MustCompile(`calendar-ovulation-dot`).MatchString(projectedDayMarkup) {
		t.Fatalf("expected projected ovulation dot on %s", projectedOvulation.Format("2006-01-02"))
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
