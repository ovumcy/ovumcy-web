package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
