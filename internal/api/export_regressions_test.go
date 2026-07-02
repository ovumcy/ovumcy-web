package api

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestExportCSVRespectsRequestedDateRange(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-range@example.com", "StrongPass1", true)

	entries := []models.DailyLog{
		{
			UserID:   user.ID,
			Date:     time.Date(2026, time.February, 2, 0, 0, 0, 0, time.UTC),
			IsPeriod: true,
			Flow:     models.FlowLight,
			Notes:    "before-range",
		},
		{
			UserID:   user.ID,
			Date:     time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC),
			IsPeriod: false,
			Flow:     models.FlowNone,
			Notes:    "in-range",
		},
		{
			UserID:   user.ID,
			Date:     time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC),
			IsPeriod: true,
			Flow:     models.FlowHeavy,
			Notes:    "after-range",
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("create export logs: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := newExportRequestForTest(t, "/api/v1/exports/csv?from=2026-02-05&to=2026-02-12", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("export csv request with range failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected header + 1 row in selected range, got %d rows", len(records))
	}

	header := records[0]
	row := records[1]
	indexByName := make(map[string]int, len(header))
	for index, name := range header {
		indexByName[name] = index
	}

	if got := row[indexByName["Date"]]; got != "2026-02-10" {
		t.Fatalf("expected in-range date 2026-02-10, got %q", got)
	}
	if got := row[indexByName["Notes"]]; got != "in-range" {
		t.Fatalf("expected in-range notes, got %q", got)
	}
}

func TestExportCSVIncludesKnownAndOtherSymptoms(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-csv@example.com", "StrongPass1", true)

	symptoms := []models.SymptomType{
		{UserID: user.ID, Name: "Cramps", Icon: "A", Color: "#111111"},
		{UserID: user.ID, Name: "Custom Symptom", Icon: "B", Color: "#222222"},
	}
	if err := database.Create(&symptoms).Error; err != nil {
		t.Fatalf("create symptoms: %v", err)
	}

	logEntry := models.DailyLog{
		UserID:          user.ID,
		Date:            time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC),
		IsPeriod:        true,
		Flow:            models.FlowLight,
		Mood:            5,
		SexActivity:     models.SexActivityUnprotected,
		BBT:             models.NewBBT(36.70),
		CervicalMucus:   models.CervicalMucusCreamy,
		CycleStart:      true,
		IsUncertain:     true,
		CycleFactorKeys: []string{models.CycleFactorStress, models.CycleFactorSleepDisruption},
		SymptomIDs:      []uint{symptoms[0].ID, symptoms[1].ID},
		Notes:           "note",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	response := exportResponseForTest(t, app, user.Email, "StrongPass1", "/api/v1/exports/csv")
	assertBodyContainsAll(t, response.Header.Get("Content-Type"),
		bodyStringMatch{fragment: "text/csv", message: "expected text/csv content type"},
	)
	assertBodyContainsAll(t, response.Header.Get("Content-Disposition"),
		bodyStringMatch{fragment: "attachment; filename=ovumcy-export-", message: "expected attachment filename header"},
	)

	records, err := csv.NewReader(strings.NewReader(mustReadBodyString(t, response.Body))).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected header + 1 row, got %d rows", len(records))
	}

	header := records[0]
	row := records[1]
	indexByName := make(map[string]int, len(header))
	for index, name := range header {
		indexByName[name] = index
	}
	assertExportCSVRowValues(t, row, indexByName, map[string]string{
		"Date":           "2026-02-18",
		"Period":         "Yes",
		"Flow":           "Light",
		"Mood rating":    "5",
		"Sex activity":   "Unprotected",
		"BBT (C)":        "36.70",
		"Cervical mucus": "Creamy",
		"Cycle factors":  "Stress; Sleep disruption",
		"Cramps":         "Yes",
		"Other":          "Custom Symptom",
		"Notes":          "note",
		"Cycle start":    "Yes",
		"Uncertain":      "Yes",
	})
}

func exportResponseForTest(t *testing.T, app *fiber.App, email string, password string, target string) *http.Response {
	t.Helper()

	authCookie := loginAndExtractAuthCookie(t, app, email, password)
	request := newExportRequestForTest(t, target, authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return response
}

func assertExportCSVRowValues(t *testing.T, row []string, indexByName map[string]int, expected map[string]string) {
	t.Helper()

	for column, want := range expected {
		index, ok := indexByName[column]
		if !ok {
			t.Fatalf("expected column %q in exported csv header", column)
		}
		if got := row[index]; got != want {
			t.Fatalf("expected %s %q, got %q", column, want, got)
		}
	}
}

func TestExportJSONNormalizesFlowAndMapsSymptoms(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-json@example.com", "StrongPass1", true)

	symptoms := []models.SymptomType{
		{UserID: user.ID, Name: "Mood swings", Icon: "A", Color: "#111111"},
		{UserID: user.ID, Name: "My Custom", Icon: "B", Color: "#222222"},
	}
	if err := database.Create(&symptoms).Error; err != nil {
		t.Fatalf("create symptoms: %v", err)
	}

	logEntry := models.DailyLog{
		UserID:          user.ID,
		Date:            time.Date(2026, time.February, 19, 0, 0, 0, 0, time.UTC),
		IsPeriod:        false,
		Flow:            "unexpected-flow",
		Mood:            4,
		SexActivity:     models.SexActivityProtected,
		BBT:             models.NewBBT(36.55),
		CervicalMucus:   models.CervicalMucusEggWhite,
		CycleStart:      true,
		IsUncertain:     true,
		CycleFactorKeys: []string{models.CycleFactorStress, models.CycleFactorTravel},
		SymptomIDs:      []uint{symptoms[0].ID, symptoms[1].ID},
		Notes:           "json-note",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	response := exportResponseForTest(t, app, user.Email, "StrongPass1", "/api/v1/exports/json")
	assertBodyContainsAll(t, response.Header.Get("Content-Type"),
		bodyStringMatch{fragment: "application/json", message: "expected application/json content type"},
	)
	assertBodyContainsAll(t, response.Header.Get("Content-Disposition"),
		bodyStringMatch{fragment: "attachment; filename=ovumcy-export-", message: "expected attachment filename header"},
	)

	payload := decodeExportJSONPayload(t, response.Body)
	assertExportJSONPayload(t, payload)
}

func decodeExportJSONPayload(t *testing.T, body io.Reader) struct {
	ExportedAt string            `json:"exported_at"`
	Entries    []exportJSONEntry `json:"entries"`
} {
	t.Helper()

	payload := struct {
		ExportedAt string            `json:"exported_at"`
		Entries    []exportJSONEntry `json:"entries"`
	}{}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatalf("decode json payload: %v", err)
	}
	return payload
}

func assertExportJSONPayload(t *testing.T, payload struct {
	ExportedAt string            `json:"exported_at"`
	Entries    []exportJSONEntry `json:"entries"`
}) {
	t.Helper()

	assertExportJSONPayloadMetadata(t, payload.ExportedAt)
	entry := assertSingleExportJSONEntry(t, payload.Entries)
	assertExportJSONTrackingFields(t, entry)
	assertExportJSONSymptomFields(t, entry)
}

func assertExportJSONPayloadMetadata(t *testing.T, exportedAt string) {
	t.Helper()

	if exportedAt == "" {
		t.Fatalf("expected exported_at in payload")
	}
	if _, err := time.Parse(time.RFC3339, exportedAt); err != nil {
		t.Fatalf("expected RFC3339 exported_at, got %q", exportedAt)
	}
}

func assertSingleExportJSONEntry(t *testing.T, entries []exportJSONEntry) exportJSONEntry {
	t.Helper()

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	return entries[0]
}

func assertExportJSONTrackingFields(t *testing.T, entry exportJSONEntry) {
	t.Helper()

	if entry.Flow != models.FlowNone {
		t.Fatalf("expected unknown flow normalized to %q, got %q", models.FlowNone, entry.Flow)
	}
	if entry.MoodRating != 4 {
		t.Fatalf("expected mood rating 4, got %d", entry.MoodRating)
	}
	if entry.SexActivity != models.SexActivityProtected {
		t.Fatalf("expected protected sex activity, got %q", entry.SexActivity)
	}
	if entry.BBT == nil || *entry.BBT != 36.55 {
		t.Fatalf("expected BBT 36.55, got %v", entry.BBT)
	}
	if entry.CervicalMucus != models.CervicalMucusEggWhite {
		t.Fatalf("expected eggwhite cervical mucus, got %q", entry.CervicalMucus)
	}
	if len(entry.CycleFactors) != 2 || entry.CycleFactors[0] != models.CycleFactorStress || entry.CycleFactors[1] != models.CycleFactorTravel {
		t.Fatalf("expected cycle factors in export json, got %#v", entry.CycleFactors)
	}
	if entry.Notes != "json-note" {
		t.Fatalf("expected notes to be preserved, got %q", entry.Notes)
	}
	if !entry.CycleStart {
		t.Fatalf("expected cycle_start=true in export json")
	}
	if !entry.IsUncertain {
		t.Fatalf("expected is_uncertain=true in export json")
	}
}

func assertExportJSONSymptomFields(t *testing.T, entry exportJSONEntry) {
	t.Helper()

	if !entry.Symptoms.Mood {
		t.Fatalf("expected mood flag to be true")
	}
	if len(entry.OtherSymptoms) != 1 || entry.OtherSymptoms[0] != "My Custom" {
		t.Fatalf("expected custom symptom in other list, got %#v", entry.OtherSymptoms)
	}
}

func TestExportSummaryRespectsRequestedDateRange(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-summary@example.com", "StrongPass1", true)

	entries := []models.DailyLog{
		{
			UserID: user.ID,
			Date:   time.Date(2026, time.February, 7, 0, 0, 0, 0, time.UTC),
			Flow:   models.FlowNone,
		},
		{
			UserID: user.ID,
			Date:   time.Date(2026, time.February, 12, 0, 0, 0, 0, time.UTC),
			Flow:   models.FlowLight,
		},
		{
			UserID: user.ID,
			Date:   time.Date(2026, time.February, 20, 0, 0, 0, 0, time.UTC),
			Flow:   models.FlowHeavy,
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("create export logs: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := newExportRequestForTest(t, "/api/v1/exports/summary?from=2026-02-10&to=2026-02-19", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("export summary request with range failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	payload := struct {
		TotalEntries int    `json:"total_entries"`
		HasData      bool   `json:"has_data"`
		DateFrom     string `json:"date_from"`
		DateTo       string `json:"date_to"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode summary payload: %v", err)
	}

	if payload.TotalEntries != 1 {
		t.Fatalf("expected total_entries 1, got %d", payload.TotalEntries)
	}
	if !payload.HasData {
		t.Fatal("expected has_data=true")
	}
	if payload.DateFrom != "2026-02-12" {
		t.Fatalf("expected date_from 2026-02-12, got %q", payload.DateFrom)
	}
	if payload.DateTo != "2026-02-12" {
		t.Fatalf("expected date_to 2026-02-12, got %q", payload.DateTo)
	}
}

func TestExportSummaryRejectsInvalidDateRange(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-summary-invalid-range@example.com", "StrongPass1", true)

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := newExportRequestForTest(t, "/api/v1/exports/summary?from=2026-02-20&to=2026-02-10", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("export summary request with invalid range failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	payload := struct {
		Error string `json:"error"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error != "invalid range" {
		t.Fatalf("expected invalid range error, got %q", payload.Error)
	}
}

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
	request := newExportRequestForTest(t, "/api/v1/exports/summary?from=2026-03-13&to=2026-03-13", joinCookieHeader(authCookie, timezoneCookieName+"=Pacific/Kiritimati"))
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
