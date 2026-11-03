package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// A future-day mark that would replace the stored start is refused: the day
// editor shows the "mark it on the day" notice instead of the cycle-start form,
// and the endpoint answers the refusal whatever replace_existing says.
func TestFutureMarkOverTheStoredStartAsksForTheMarkOnTheDay(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "mark-on-the-day@example.com", "StrongPass1", true)
	today := seedCycleStartAnchorForDayEditor(t, database, user.ID, 0)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("last_period_start", today).Error; err != nil {
		t.Fatalf("set stored start: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	tomorrow := today.AddDate(0, 0, 1)

	markup := fetchDayEditorMarkup(t, app, authCookie, tomorrow)
	if !strings.Contains(markup, "data-cycle-start-on-the-day") {
		t.Fatal("expected the mark-on-the-day notice on tomorrow's editor")
	}
	if strings.Contains(markup, "data-day-cycle-start-button") {
		t.Fatal("expected no cycle-start form beside the mark-on-the-day notice")
	}
	if !strings.Contains(markup, "Mark it on the day.") {
		t.Fatal("expected the notice copy in the day editor")
	}
	assertNoFutureNoticeBesideMarkOnTheDay(t, "edit view", markup)
	assertNoFutureNoticeBesideMarkOnTheDay(t, "no-entry summary", fetchDaySummaryMarkup(t, app, authCookie, tomorrow))
	if err := database.Create(&models.DailyLog{UserID: user.ID, Date: tomorrow, Notes: "planned"}).Error; err != nil {
		t.Fatalf("seed tomorrow's entry: %v", err)
	}
	withData := fetchDaySummaryMarkup(t, app, authCookie, tomorrow)
	if !strings.Contains(withData, "data-day-editor-open") || strings.Contains(withData, "No record was kept.") {
		t.Fatal("expected tomorrow's summary to render the with-data branch")
	}
	assertNoFutureNoticeBesideMarkOnTheDay(t, "with-data summary", withData)

	// With the stored start's period well behind it, tomorrow keeps its
	// forecast notice.
	open := createOnboardingTestUser(t, database, "mark-on-the-day-open@example.com", "StrongPass1", true)
	openStart := seedCycleStartAnchorForDayEditor(t, database, open.ID, 10).AddDate(0, 0, -10)
	if err := database.Model(&models.User{}).Where("id = ?", open.ID).Update("last_period_start", openStart).Error; err != nil {
		t.Fatalf("set stored start: %v", err)
	}
	openMarkup := fetchDayEditorMarkup(t, app, loginAndExtractAuthCookie(t, app, open.Email, "StrongPass1"), tomorrow)
	if !strings.Contains(openMarkup, "data-future-cycle-start-notice") || strings.Contains(openMarkup, "data-cycle-start-on-the-day") {
		t.Fatal("expected the future notice, and no mark-on-the-day notice, when tomorrow is clear of the stored start")
	}

	form := url.Values{"replace_existing": {"true"}, "mark_uncertain": {"true"}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/days/"+tomorrow.Format("2006-01-02")+"/cycle-start", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=UTC"))
	request.Header.Set(timezoneHeaderName, "UTC")
	response := mustAppResponse(t, app, request)
	defer func() { _ = response.Body.Close() }()
	assertStatusCode(t, response, http.StatusBadRequest)
	if got := readAPIError(t, response.Body); got != "cycle start mark on the day" {
		t.Fatalf("expected the mark-on-the-day refusal, got %q", got)
	}

	var stored models.User
	if err := database.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if stored.LastPeriodStart == nil || !services.CalendarDay(*stored.LastPeriodStart, nil).Equal(today) {
		t.Fatalf("stored start = %v, want it unchanged at %s", stored.LastPeriodStart, today.Format("2006-01-02"))
	}
}

// The forecast notice says a future mark will recalculate predictions; beside
// a refusal to mark that day it would contradict it.
func assertNoFutureNoticeBesideMarkOnTheDay(t *testing.T, view, markup string) {
	t.Helper()
	if !strings.Contains(markup, "data-cycle-start-on-the-day") {
		t.Fatalf("%s: expected the mark-on-the-day notice", view)
	}
	if strings.Contains(markup, "data-future-cycle-start-notice") {
		t.Fatalf("%s: expected no future-prediction notice beside the mark-on-the-day notice", view)
	}
}

func fetchDaySummaryMarkup(t *testing.T, app *fiber.App, authCookie string, day time.Time) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/calendar/day/"+day.Format("2006-01-02"), nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=UTC"))
	request.Header.Set(timezoneHeaderName, "UTC")
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}
