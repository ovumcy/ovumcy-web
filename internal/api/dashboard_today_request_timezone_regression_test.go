package api

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestBuildDashboardViewDataKeepsMarchThreeAtUTCBoundary(t *testing.T) {
	handler, database := newDataAccessTestHandler(t)
	user := createDataAccessTestUser(t, database, "dashboard-tz-fixed-boundary@example.com")

	location := time.FixedZone("UTC+3", 3*60*60)
	now := time.Date(2026, time.March, 2, 21, 30, 0, 0, time.UTC)

	data, err := handler.buildDashboardViewData(&user, "ru", map[string]string{}, now, location)
	if err != nil {
		t.Fatalf("build dashboard view data: %v", err)
	}

	expectedDay := services.DateAtLocation(now, location)
	expectedRaw := expectedDay.Format("2006-01-02")
	expectedFormatted := services.LocalizedDashboardDate("ru", expectedDay)

	gotRaw, ok := data["Today"].(string)
	if !ok {
		t.Fatalf("expected Today field in dashboard payload")
	}
	if gotRaw != expectedRaw {
		t.Fatalf("expected today %q, got %q", expectedRaw, gotRaw)
	}

	gotFormatted, ok := data["FormattedDate"].(string)
	if !ok {
		t.Fatalf("expected FormattedDate field in dashboard payload")
	}
	if gotFormatted != expectedFormatted {
		t.Fatalf("expected formatted date %q, got %q", expectedFormatted, gotFormatted)
	}
}

func TestDashboardTodayActionsUseRequestTimezoneHeaderAndCookie(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-tz-actions@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	today := services.DateAtLocation(nowUTC.In(location), location)
	todayRaw := today.Format("2006-01-02")
	formattedToday := services.LocalizedDashboardDate("ru", today)

	seed := models.DailyLog{
		UserID:   user.ID,
		Date:     today,
		IsPeriod: true,
		Flow:     models.FlowNone,
		Notes:    "timezone clear seed",
	}
	if err := database.Create(&seed).Error; err != nil {
		t.Fatalf("seed today daily log: %v", err)
	}

	dashboardResponse := dashboardWithTimezoneResponse(t, app, authCookie, timezoneName)
	rendered := mustReadBodyString(t, dashboardResponse.Body)

	if !strings.Contains(rendered, formattedToday) {
		t.Fatalf("expected dashboard header date %q, got different value", formattedToday)
	}

	expectedSavePath := fmt.Sprintf(`hx-post="/api/days/%s"`, todayRaw)
	if !strings.Contains(rendered, expectedSavePath) {
		t.Fatalf("expected save action %q in dashboard", expectedSavePath)
	}

	expectedClearPath := fmt.Sprintf(`hx-delete="/api/log/delete?date=%s&source=dashboard"`, todayRaw)
	if !strings.Contains(rendered, expectedClearPath) {
		t.Fatalf("expected clear action %q in dashboard", expectedClearPath)
	}

	tzCookie := responseCookie(dashboardResponse.Cookies(), timezoneCookieName)
	if tzCookie == nil || strings.TrimSpace(tzCookie.Value) == "" {
		t.Fatalf("expected timezone cookie %q in dashboard response", timezoneCookieName)
	}
	tzCookieHeader := authCookie + "; " + timezoneCookieName + "=" + tzCookie.Value

	clearResponse := dashboardTimezoneActionResponse(t, app, http.MethodDelete, "/api/log/delete?date="+todayRaw+"&source=dashboard", nil, tzCookieHeader)
	assertStatusCode(t, clearResponse, http.StatusOK)
	if clearResponse.Header.Get("HX-Redirect") != "/dashboard" {
		t.Fatalf("expected HX-Redirect /dashboard on clear, got %q", clearResponse.Header.Get("HX-Redirect"))
	}

	clearedEntry, err := fetchLogByDateForTest(database, user.ID, today, location)
	if err != nil {
		t.Fatalf("load day after clear: %v", err)
	}
	if clearedEntry.ID != 0 {
		t.Fatalf("expected today entry to be removed after clear, got id=%d", clearedEntry.ID)
	}

	form := url.Values{
		"is_period": {"true"},
		"flow":      {models.FlowNone},
		"notes":     {"timezone save note"},
	}
	saveResponse := dashboardTimezoneActionResponse(t, app, http.MethodPost, "/api/days/"+todayRaw, strings.NewReader(form.Encode()), tzCookieHeader)
	assertStatusCode(t, saveResponse, http.StatusOK)

	savedEntry, err := fetchLogByDateForTest(database, user.ID, today, location)
	if err != nil {
		t.Fatalf("load day after save: %v", err)
	}
	if savedEntry.ID == 0 {
		t.Fatal("expected saved entry for local today")
	}
	if savedEntry.Notes != "timezone save note" {
		t.Fatalf("expected saved notes %q, got %q", "timezone save note", savedEntry.Notes)
	}
}

func dashboardWithTimezoneResponse(t *testing.T, app *fiber.App, authCookie string, timezoneName string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "ru")
	request.Header.Set("Cookie", authCookie)
	request.Header.Set(timezoneHeaderName, timezoneName)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return response
}

func dashboardTimezoneActionResponse(t *testing.T, app *fiber.App, method string, target string, body io.Reader, cookieHeader string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(method, target, body)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "ru")
	request.Header.Set("Cookie", cookieHeader)

	return mustAppResponse(t, app, request)
}

func timezoneWithDifferentCalendarDay(t *testing.T, nowUTC time.Time) (string, *time.Location) {
	t.Helper()

	utcDay := services.DateAtLocation(nowUTC, time.UTC).Format("2006-01-02")
	candidates := []string{
		"Pacific/Kiritimati",
		"Pacific/Pago_Pago",
		"Pacific/Auckland",
		"America/Adak",
		"Europe/Moscow",
	}

	for _, name := range candidates {
		location, err := time.LoadLocation(name)
		if err != nil {
			continue
		}
		localDay := services.DateAtLocation(nowUTC.In(location), location).Format("2006-01-02")
		if localDay != utcDay {
			return name, location
		}
	}

	t.Skip("timezone data unavailable for different-calendar-day regression")
	return "", nil
}
