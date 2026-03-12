package api

import (
	"encoding/csv"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
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
	request := newExportRequestForTest(t, "/api/export/csv?from=2026-02-05&to=2026-02-12", authCookie)

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
