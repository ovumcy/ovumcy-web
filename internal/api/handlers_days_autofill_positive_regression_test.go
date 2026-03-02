package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestUpsertDayAutoFillsFollowingPeriodDays(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-autofill@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"period_length":    4,
		"auto_period_fill": true,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}

	payload := map[string]any{
		"is_period":   true,
		"flow":        models.FlowMedium,
		"symptom_ids": []uint{},
		"notes":       "",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/days/2026-02-10", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	autoFilledDays := []string{"2026-02-10", "2026-02-11", "2026-02-12", "2026-02-13"}
	for _, dateRaw := range autoFilledDays {
		day, err := services.ParseDayDate(dateRaw, time.UTC)
		if err != nil {
			t.Fatalf("parse day %s: %v", dateRaw, err)
		}
		entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
		if err != nil {
			t.Fatalf("fetch log for %s: %v", dateRaw, err)
		}
		if !entry.IsPeriod {
			t.Fatalf("expected %s to be auto-marked as period day", dateRaw)
		}
	}

	dayAfterAutoFill, err := services.ParseDayDate("2026-02-14", time.UTC)
	if err != nil {
		t.Fatalf("parse day after auto-fill: %v", err)
	}
	dayAfterEntry, err := fetchLogByDateForTest(database, user.ID, dayAfterAutoFill, time.UTC)
	if err != nil {
		t.Fatalf("fetch log for day after auto-fill: %v", err)
	}
	if dayAfterEntry.IsPeriod {
		t.Fatalf("expected day after auto-fill window to remain non-period")
	}
}
