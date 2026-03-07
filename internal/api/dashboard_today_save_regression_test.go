package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestDashboardTodaySavePersistsPeriodToggleAndNotes(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-today-save@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	todayRaw := today.Format("2006-01-02")
	note := "Remember hydration and rest"

	form := url.Values{
		"is_period": {"true"},
		"flow":      {models.FlowNone},
		"notes":     {note},
	}
	saveResponse := mustAppResponse(t, app, dashboardSaveRequest(todayRaw, form, authCookie))
	assertStatusCode(t, saveResponse, http.StatusOK)

	saveBody := mustReadBodyString(t, saveResponse.Body)
	assertBodyContainsAll(t, saveBody,
		bodyStringMatch{fragment: "status-ok", message: "expected save status success markup"},
		bodyStringMatch{fragment: "data-dismiss-status", message: "expected dismiss button marker in save status markup"},
	)

	parsedDay, err := services.ParseDayDate(todayRaw, time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, parsedDay, time.UTC)
	if err != nil {
		t.Fatalf("load stored day after dashboard save: %v", err)
	}
	if !entry.IsPeriod {
		t.Fatal("expected period toggle to persist after dashboard save")
	}
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected flow to remain %q, got %q", models.FlowNone, entry.Flow)
	}
	if entry.Notes != note {
		t.Fatalf("expected notes %q, got %q", note, entry.Notes)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)
	dashboardResponse := mustAppResponse(t, app, dashboardRequest)
	assertStatusCode(t, dashboardResponse, http.StatusOK)

	rendered := mustReadBodyString(t, dashboardResponse.Body)
	periodCheckedPattern := regexp.MustCompile(`(?s)name="is_period"[^>]*checked`)
	if !periodCheckedPattern.MatchString(rendered) {
		t.Fatalf("expected dashboard period toggle to remain checked after reload")
	}
	if !strings.Contains(rendered, note) {
		t.Fatalf("expected dashboard notes field to include saved note %q", note)
	}
}

func dashboardSaveRequest(todayRaw string, form url.Values, authCookie string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/days/"+todayRaw, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	return request
}
