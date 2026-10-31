package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestPredictionSurfacesRenderTheirQualifiedKeys pins WEB-390 at the render
// layer: on a history steady enough that no Approximate caption renders, the
// calendar's fertile-window legend entry, the dashboard's fertility badge and
// its next-period days still render from the catalogue keys that
// TestPredictionSurfacesCarryAnEstimateQualifier proves qualified in every locale.
func TestPredictionSurfacesRenderTheirQualifiedKeys(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "estimate-qualifier@example.com", "StrongPass1", true)
	periodStart := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -4)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": periodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}
	for _, cycleStart := range []time.Time{periodStart.AddDate(0, 0, -84), periodStart.AddDate(0, 0, -56), periodStart.AddDate(0, 0, -28), periodStart} {
		for offset := range 5 {
			entry := models.DailyLog{
				UserID:   user.ID,
				Date:     cycleStart.AddDate(0, 0, offset),
				IsPeriod: true,
				Flow:     models.FlowMedium,
			}
			if cycleStart.Equal(periodStart) && offset == 4 {
				// Today's egg-white observation is what raises the fertility badge.
				entry.CervicalMucus = models.CervicalMucusEggWhite
			}
			if err := database.Create(&entry).Error; err != nil {
				t.Fatalf("create period log: %v", err)
			}
		}
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	calendar := mustParseHTMLDocument(t, renderCalendarMonthHTML(t, app, authCookie, periodStart.AddDate(0, 0, 13).Format("2006-01")))
	legend := htmlFindElement(calendar, htmlNodeHasAttr("data-calendar-legend"))
	if legend == nil {
		t.Fatal("expected the calendar legend")
	}
	if fertile := htmlFindElement(legend, htmlNodeHasAttr("data-legend-key")); fertile == nil || htmlAttr(fertile, "data-legend-key") != "calendar.legend.fertility" {
		t.Fatal("expected the fertile-window legend entry rendered from calendar.legend.fertility")
	}

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	dashboard := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	nextPeriod := htmlFindElement(dashboard, htmlNodeHasAttr("data-dashboard-next-period"))
	if nextPeriod == nil {
		t.Fatal("expected the next-period estimate on the dashboard")
	}
	if dates := htmlFindElement(nextPeriod, htmlNodeHasAttr("data-next-period-key")); dates == nil || htmlAttr(dates, "data-next-period-key") != "dashboard.next_period_days" {
		t.Fatal("expected the next-period days rendered from dashboard.next_period_days")
	}
	if badge := htmlFindElement(dashboard, htmlNodeHasAttr("data-fertility-badge")); badge == nil || htmlAttr(badge, "data-fertility-badge-key") != "dashboard.high_fertility_badge" {
		t.Fatal("expected the fertility badge rendered from dashboard.high_fertility_badge")
	}
	if htmlFindElement(dashboard, htmlNodeHasAttr("data-dashboard-estimate-qualifier")) != nil {
		t.Fatal("expected a steady history to render no Approximate caption, so the inline qualifier is the only one")
	}
}
