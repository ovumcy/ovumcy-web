package api

import (
	"io"
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
	saveRequest := httptest.NewRequest(http.MethodPost, "/api/days/"+todayRaw, strings.NewReader(form.Encode()))
	saveRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveRequest.Header.Set("HX-Request", "true")
	saveRequest.Header.Set("Accept-Language", "en")
	saveRequest.Header.Set("Cookie", authCookie)

	saveResponse, err := app.Test(saveRequest, -1)
	if err != nil {
		t.Fatalf("dashboard save request failed: %v", err)
	}
	defer saveResponse.Body.Close()

	if saveResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", saveResponse.StatusCode)
	}

	saveBody, err := io.ReadAll(saveResponse.Body)
	if err != nil {
		t.Fatalf("read save response body: %v", err)
	}
	if !strings.Contains(string(saveBody), "status-ok") {
		t.Fatalf("expected save status success markup, got %q", string(saveBody))
	}
	if !strings.Contains(string(saveBody), "data-dismiss-status") {
		t.Fatalf("expected dismiss button marker in save status markup, got %q", string(saveBody))
	}

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

	dashboardResponse, err := app.Test(dashboardRequest, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer dashboardResponse.Body.Close()

	if dashboardResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", dashboardResponse.StatusCode)
	}

	dashboardBody, err := io.ReadAll(dashboardResponse.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	rendered := string(dashboardBody)
	periodCheckedPattern := regexp.MustCompile(`(?s)name="is_period"[^>]*checked`)
	if !periodCheckedPattern.MatchString(rendered) {
		t.Fatalf("expected dashboard period toggle to remain checked after reload")
	}
	if !strings.Contains(rendered, note) {
		t.Fatalf("expected dashboard notes field to include saved note %q", note)
	}
}
