package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestPartnerReadOnlyFlowSmoke(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "smoke-partner@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("role", models.RolePartner).Error; err != nil {
		t.Fatalf("set partner role: %v", err)
	}

	logEntry := models.DailyLog{
		UserID:     user.ID,
		Date:       time.Date(2026, time.February, 21, 0, 0, 0, 0, time.UTC),
		IsPeriod:   true,
		Flow:       models.FlowMedium,
		SymptomIDs: []uint{1, 2},
		Notes:      "partner-private-note",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create partner log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	dashboardBody := smokeGET(t, app, authCookie, "/dashboard", http.StatusOK)
	if strings.Contains(dashboardBody, `hx-post="/api/days/`) {
		t.Fatalf("expected partner dashboard to be read-only")
	}

	settingsBody := smokeGET(t, app, authCookie, "/settings", http.StatusOK)
	if strings.Contains(settingsBody, `data-export-section`) {
		t.Fatalf("expected owner-only export section hidden for partner")
	}
	if strings.Contains(settingsBody, `action="/settings/cycle"`) {
		t.Fatalf("expected owner-only cycle settings hidden for partner")
	}

	dayPanelBody := smokeGET(t, app, authCookie, "/calendar/day/2026-02-21", http.StatusOK)
	if strings.Contains(dayPanelBody, `hx-post="/api/days/2026-02-21"`) {
		t.Fatalf("expected partner day panel to be read-only")
	}
	if strings.Contains(dayPanelBody, `name="notes"`) {
		t.Fatalf("expected partner day notes field to be hidden")
	}
	if strings.Contains(dayPanelBody, `name="symptom_ids"`) {
		t.Fatalf("expected partner day symptoms controls to be hidden")
	}

	payload := map[string]any{
		"is_period":   true,
		"flow":        models.FlowMedium,
		"symptom_ids": []uint{},
		"notes":       "forbidden write",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal partner payload: %v", err)
	}

	upsertRequest := httptest.NewRequest(http.MethodPost, "/api/days/2026-02-21", bytes.NewReader(body))
	upsertRequest.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	upsertRequest.Header.Set("Cookie", authCookie)

	upsertResponse, err := app.Test(upsertRequest, -1)
	if err != nil {
		t.Fatalf("partner upsert request failed: %v", err)
	}
	defer upsertResponse.Body.Close()

	if upsertResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("expected partner upsert status 403, got %d", upsertResponse.StatusCode)
	}

	exportRequest := newExportRequestForTest(t, "/api/export/csv?from=2026-02-01&to=2026-02-28", authCookie)

	exportResponse, err := app.Test(exportRequest, -1)
	if err != nil {
		t.Fatalf("partner export request failed: %v", err)
	}
	defer exportResponse.Body.Close()

	if exportResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("expected partner export status 403, got %d", exportResponse.StatusCode)
	}
}
