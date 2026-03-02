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

func TestUpsertDayAutoFillCanBeDisabled(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-autofill-disabled@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"period_length":    4,
		"auto_period_fill": false,
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

	firstDay, err := services.ParseDayDate("2026-02-10", time.UTC)
	if err != nil {
		t.Fatalf("parse first day: %v", err)
	}
	firstEntry, err := fetchLogByDateForTest(database, user.ID, firstDay, time.UTC)
	if err != nil {
		t.Fatalf("fetch first day log: %v", err)
	}
	if !firstEntry.IsPeriod {
		t.Fatalf("expected first selected day to be period")
	}

	nextDay, err := services.ParseDayDate("2026-02-11", time.UTC)
	if err != nil {
		t.Fatalf("parse next day: %v", err)
	}
	nextEntry, err := fetchLogByDateForTest(database, user.ID, nextDay, time.UTC)
	if err != nil {
		t.Fatalf("fetch next day log: %v", err)
	}
	if nextEntry.IsPeriod {
		t.Fatalf("expected next day to stay manual when auto-fill is disabled")
	}
}
