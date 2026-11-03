package api

import (
	"encoding/csv"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// An account that finishes setup with auto period fill off has one logged
// cycle start on the date it entered: the export file, the day panel and the
// calendar describe the same day. Legacy accounts (a stored start with no row)
// are covered by the export and panel tests that seed the bare column.
func TestOnboardingStartIsALoggedCycleStartInTheExportAndThePanel(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-logged-start@example.com", "StrongPass1", false)
	now := time.Now().UTC().AddDate(0, 0, -3)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"auto_period_fill":  false,
		"last_period_start": start,
	}).Error; err != nil {
		t.Fatalf("store the setup answers: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	complete := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(""))
	complete.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	complete.Header.Set("HX-Request", "true")
	complete.Header.Set("Cookie", authCookie)
	bindDayWriteForTest(t, complete, user.ID)
	response := mustAppResponse(t, app, complete)
	assertStatusCode(t, response, http.StatusOK)
	_ = response.Body.Close()

	day := start.Format("2006-01-02")

	csvResponse := mustAppResponse(t, app, newExportRequestForTest(t, "/api/v1/exports/csv", authCookie))
	assertStatusCode(t, csvResponse, http.StatusOK)
	body, err := io.ReadAll(csvResponse.Body)
	_ = csvResponse.Body.Close()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected header + the start day, got %d records: %v", len(records), records)
	}
	indexByName := make(map[string]int, len(records[0]))
	for index, name := range records[0] {
		indexByName[name] = index
	}
	assertExportCSVRowValues(t, records[1], indexByName, map[string]string{"Date": day, "Cycle start": "Yes", "Period": "Yes"})

	payload := decodeExportJSONObject(t, mustAppResponse(t, app, newExportRequestForTest(t, "/api/v1/exports/json", authCookie)))
	entries, _ := payload["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("JSON export carries %d entries, want the start day: %v", len(entries), payload["entries"])
	}
	entry, _ := entries[0].(map[string]any)
	if entry["date"] != day || entry["period"] != true {
		t.Fatalf("JSON entry = %v, want a period entry on %s", entry, day)
	}

	panelRequest := httptest.NewRequest(http.MethodGet, "/calendar/day/"+day, nil)
	panelRequest.Header.Set("Accept-Language", "en")
	panelRequest.Header.Set("Cookie", authCookie)
	panelResponse := mustAppResponse(t, app, panelRequest)
	assertStatusCode(t, panelResponse, http.StatusOK)
	panel := mustReadBodyString(t, panelResponse.Body)
	if strings.Contains(panel, "No record was kept.") {
		t.Fatalf("the start day panel of a new account still says no record was kept: %q", panel)
	}
}
