package api

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The owner's stored onboarding start is a cycle boundary no logged day
// carries. These pin it to the wire: the JSON export names it, the CSV marks
// that date as a cycle start, and a restore puts it back on the account.

func seedOnboardingStartExportUser(t *testing.T, email string) func(target string) *http.Response {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
	onboardingStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("last_period_start", onboardingStart).Error; err != nil {
		t.Fatalf("store onboarding start: %v", err)
	}
	logEntry := models.DailyLog{UserID: user.ID, Date: time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowMedium}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	fetch := func(target string) *http.Response {
		response := mustAppResponse(t, app, newExportRequestForTest(t, target, authCookie))
		assertStatusCode(t, response, http.StatusOK)
		return response
	}
	return fetch
}

func decodeExportJSONObject(t *testing.T, response *http.Response) map[string]any {
	t.Helper()
	defer func() { _ = response.Body.Close() }()

	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode json payload: %v", err)
	}
	return payload
}

func TestExportJSONCarriesTheOnboardingStart(t *testing.T) {
	t.Parallel()

	fetch := seedOnboardingStartExportUser(t, "export-onboarding-json@example.com")

	payload := decodeExportJSONObject(t, fetch("/api/v1/exports/json"))
	if got, _ := payload["last_period_start"].(string); got != "2026-03-01" {
		t.Fatalf("last_period_start = %v, want 2026-03-01", payload["last_period_start"])
	}

	// A range that leaves the date out leaves the field out, like a day.
	ranged := decodeExportJSONObject(t, fetch("/api/v1/exports/json?from=2026-03-05&to=2026-03-31"))
	if value, present := ranged["last_period_start"]; present {
		t.Fatalf("ranged export carries last_period_start = %v, want the field absent", value)
	}
}

func TestExportCSVMarksTheOnboardingStartAsACycleStart(t *testing.T) {
	t.Parallel()

	fetch := seedOnboardingStartExportUser(t, "export-onboarding-csv@example.com")

	response := fetch("/api/v1/exports/csv")
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected header + onboarding row + logged row, got %d records: %v", len(records), records)
	}
	indexByName := make(map[string]int, len(records[0]))
	for index, name := range records[0] {
		indexByName[name] = index
	}
	assertExportCSVRowValues(t, records[1], indexByName, map[string]string{"Date": "2026-03-01", "Cycle start": "Yes", "Period": "No"})
	assertExportCSVRowValues(t, records[2], indexByName, map[string]string{"Date": "2026-03-20", "Cycle start": "No", "Period": "Yes"})
}

func TestImportJSONRestoresTheOnboardingStart(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "import-onboarding-start@example.com")
	body := `{"last_period_start":"2026-03-01","entries":[` +
		`{"date":"2026-03-20","period":true,"flow":"medium","cycle_factors":[]}` +
		`]}`

	response, err := ctx.app.Test(newImportRequest(ctx, body, true), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}

	var restored models.User
	if err := ctx.database.First(&restored, ctx.user.ID).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if restored.LastPeriodStart == nil || restored.LastPeriodStart.UTC().Format("2006-01-02") != "2026-03-01" {
		t.Fatalf("restored last_period_start = %v, want 2026-03-01", restored.LastPeriodStart)
	}
}
