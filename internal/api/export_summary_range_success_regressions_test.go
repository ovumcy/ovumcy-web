package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

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
	request := newExportRequestForTest(t, "/api/export/summary?from=2026-02-10&to=2026-02-19", authCookie)

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
