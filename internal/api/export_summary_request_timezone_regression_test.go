package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestExportSummaryUsesRequestTimezoneForRangeParsing(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-summary-timezone@example.com", "StrongPass1", true)

	location, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Skipf("load Pacific/Kiritimati timezone: %v", err)
	}

	localDay := time.Date(2026, time.March, 13, 0, 0, 0, 0, location)
	if err := database.Create(&models.DailyLog{
		UserID: user.ID,
		Date:   localDay,
		Flow:   models.FlowLight,
	}).Error; err != nil {
		t.Fatalf("create timezone export log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := newExportRequestForTest(t, "/api/export/summary?from=2026-03-13&to=2026-03-13", joinCookieHeader(authCookie, timezoneCookieName+"=Pacific/Kiritimati"))
	request.Header.Set(timezoneHeaderName, "Pacific/Kiritimati")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("timezone export summary request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read timezone export summary body: %v", err)
	}

	payload := struct {
		TotalEntries int    `json:"total_entries"`
		HasData      bool   `json:"has_data"`
		DateFrom     string `json:"date_from"`
		DateTo       string `json:"date_to"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode timezone export summary payload: %v", err)
	}

	if payload.TotalEntries != 1 || !payload.HasData {
		t.Fatalf("expected one entry for request-local export day, got %#v", payload)
	}
	if payload.DateFrom != "2026-03-13" || payload.DateTo != "2026-03-13" {
		t.Fatalf("expected request-local summary day 2026-03-13, got %#v", payload)
	}
}
