package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestSettingsPageExportDefaultsToRequestLocalTodayWhenFutureEntriesExist(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-export-defaults@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	requestLocalToday := services.DateAtLocation(nowUTC.In(location), location)
	pastDay := requestLocalToday.AddDate(0, 0, -2)
	futureDay := requestLocalToday.AddDate(0, 0, 4)

	logs := []models.DailyLog{
		{UserID: user.ID, Date: pastDay, Flow: models.FlowLight},
		{UserID: user.ID, Date: futureDay, Flow: models.FlowHeavy},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create export default logs: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName))
	request.Header.Set(timezoneHeaderName, timezoneName)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	rendered := mustReadBodyString(t, response.Body)

	pastISO := pastDay.Format("2006-01-02")
	todayISO := requestLocalToday.Format("2006-01-02")
	futureISO := futureDay.Format("2006-01-02")

	exportToPattern := regexp.MustCompile(`(?s)id="export-to".*?value="` + regexp.QuoteMeta(todayISO) + `".*?max="` + regexp.QuoteMeta(futureISO) + `"`)
	if !exportToPattern.MatchString(rendered) {
		t.Fatalf("expected export-to default %q with max %q, got %q", todayISO, futureISO, rendered)
	}

	exportFromPattern := regexp.MustCompile(`(?s)id="export-from".*?value="` + regexp.QuoteMeta(pastISO) + `".*?min="` + regexp.QuoteMeta(pastISO) + `"`)
	if !exportFromPattern.MatchString(rendered) {
		t.Fatalf("expected export-from default/min %q, got %q", pastISO, rendered)
	}
}
