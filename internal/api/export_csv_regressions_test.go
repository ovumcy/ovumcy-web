package api

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
)

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
		UserID:     user.ID,
		Date:       time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC),
		IsPeriod:   true,
		Flow:       models.FlowLight,
		SymptomIDs: []uint{symptoms[0].ID, symptoms[1].ID},
		Notes:      "note",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	response := exportResponseForTest(t, app, user.Email, "StrongPass1", "/api/export/csv")
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

	if got := row[indexByName["Date"]]; got != "2026-02-18" {
		t.Fatalf("expected date 2026-02-18, got %q", got)
	}
	if got := row[indexByName["Period"]]; got != "Yes" {
		t.Fatalf("expected period Yes, got %q", got)
	}
	if got := row[indexByName["Flow"]]; got != "Light" {
		t.Fatalf("expected flow Light, got %q", got)
	}
	if got := row[indexByName["Cramps"]]; got != "Yes" {
		t.Fatalf("expected Cramps Yes, got %q", got)
	}
	if got := row[indexByName["Other"]]; got != "Custom Symptom" {
		t.Fatalf("expected Other to include custom symptom, got %q", got)
	}
	if got := row[indexByName["Notes"]]; got != "note" {
		t.Fatalf("expected notes to be exported, got %q", got)
	}
}

func exportResponseForTest(t *testing.T, app *fiber.App, email string, password string, target string) *http.Response {
	t.Helper()

	authCookie := loginAndExtractAuthCookie(t, app, email, password)
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return response
}
