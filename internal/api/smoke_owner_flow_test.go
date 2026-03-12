package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestOwnerCriticalFlowSmoke(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "smoke-owner@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	smokeGET(t, app, authCookie, "/dashboard", http.StatusOK)

	settingsBody := smokeGET(t, app, authCookie, "/settings", http.StatusOK)
	if !strings.Contains(settingsBody, `data-export-section`) {
		t.Fatalf("expected settings page to include export section")
	}

	calendarBody := smokeGET(t, app, authCookie, "/calendar?month=2026-02", http.StatusOK)
	if !strings.Contains(calendarBody, `id="day-editor"`) {
		t.Fatalf("expected calendar page to render day editor target")
	}

	payload := map[string]any{
		"is_period":   true,
		"flow":        models.FlowMedium,
		"symptom_ids": []uint{},
		"notes":       "smoke flow note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal day payload: %v", err)
	}

	upsertRequest := httptest.NewRequest(http.MethodPost, "/api/days/2026-02-21", bytes.NewReader(body))
	upsertRequest.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	upsertRequest.Header.Set("Cookie", authCookie)

	upsertResponse, err := app.Test(upsertRequest, -1)
	if err != nil {
		t.Fatalf("upsert day request failed: %v", err)
	}
	defer upsertResponse.Body.Close()

	if upsertResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected upsert status 200, got %d", upsertResponse.StatusCode)
	}

	dayPanelBody := smokeGET(t, app, authCookie, "/calendar/day/2026-02-21", http.StatusOK)
	if !strings.Contains(dayPanelBody, "smoke flow note") {
		t.Fatalf("expected day panel to include persisted day notes")
	}

	exportRequest := newExportRequestForTest(t, "/api/export/csv?from=2026-02-01&to=2026-02-28", authCookie)

	exportResponse, err := app.Test(exportRequest, -1)
	if err != nil {
		t.Fatalf("export request failed: %v", err)
	}
	defer exportResponse.Body.Close()

	if exportResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected export status 200, got %d", exportResponse.StatusCode)
	}
	if got := exportResponse.Header.Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("expected export content type text/csv, got %q", got)
	}

	exportBody, err := io.ReadAll(exportResponse.Body)
	if err != nil {
		t.Fatalf("read export body: %v", err)
	}
	renderedExport := string(exportBody)
	if !strings.Contains(renderedExport, "2026-02-21") {
		t.Fatalf("expected exported csv to include saved day date")
	}
	if !strings.Contains(renderedExport, "smoke flow note") {
		t.Fatalf("expected exported csv to include saved day notes")
	}
}
