package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestUpsertDayNormalizesFlowWhenNotPeriod(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-normalize@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	payload := map[string]any{
		"is_period":   false,
		"flow":        models.FlowHeavy,
		"symptom_ids": []uint{},
		"notes":       "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-19", bytes.NewReader(body))
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

	day, err := services.ParseDayDate("2026-02-19", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected non-period flow normalized to %q, got %q", models.FlowNone, entry.Flow)
	}
}

func TestUpsertDayAllowsPeriodWithoutExplicitFlow(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-invalid-flow@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	payload := map[string]any{
		"is_period":   true,
		"flow":        models.FlowNone,
		"symptom_ids": []uint{},
		"notes":       "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-19", bytes.NewReader(body))
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

	day, err := services.ParseDayDate("2026-02-19", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if !entry.IsPeriod {
		t.Fatal("expected period day to persist when flow is none")
	}
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected stored flow %q, got %q", models.FlowNone, entry.Flow)
	}
}

func TestUpsertDayPreservesSymptomsWhenNotPeriod(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-clear-symptoms@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	symptom := models.SymptomType{
		UserID:    user.ID,
		Name:      "Cramps",
		Icon:      "🩸",
		Color:     "#FF4444",
		IsBuiltin: true,
	}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create symptom: %v", err)
	}

	payload := map[string]any{
		"is_period":   false,
		"flow":        models.FlowLight,
		"symptom_ids": []uint{symptom.ID},
		"notes":       "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-20", bytes.NewReader(body))
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

	day, err := services.ParseDayDate("2026-02-20", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if len(entry.SymptomIDs) != 1 || entry.SymptomIDs[0] != symptom.ID {
		t.Fatalf("expected symptoms to stay persisted for non-period day, got %v", entry.SymptomIDs)
	}
}

func TestUpsertDayPersistsPregnancyTest(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-pregnancy-test@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	payload := map[string]any{
		"is_period":      false,
		"flow":           models.FlowNone,
		"pregnancy_test": models.PregnancyTestPositive,
		"symptom_ids":    []uint{},
		"notes":          "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-21", bytes.NewReader(body))
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

	day, err := services.ParseDayDate("2026-02-21", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if entry.PregnancyTest != models.PregnancyTestPositive {
		t.Fatalf("expected stored pregnancy test %q, got %q", models.PregnancyTestPositive, entry.PregnancyTest)
	}
}

func TestUpsertDayNormalizesUnknownPregnancyTest(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-pregnancy-normalize@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	payload := map[string]any{
		"is_period":      false,
		"flow":           models.FlowNone,
		"pregnancy_test": "bogus-value",
		"symptom_ids":    []uint{},
		"notes":          "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-22", bytes.NewReader(body))
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

	day, err := services.ParseDayDate("2026-02-22", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if entry.PregnancyTest != models.PregnancyTestNone {
		t.Fatalf("expected unknown pregnancy test normalized to %q, got %q", models.PregnancyTestNone, entry.PregnancyTest)
	}
}

func TestUpsertDayPersistsLHTest(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-lh-test@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	payload := map[string]any{
		"is_period":   false,
		"flow":        models.FlowNone,
		"lh_test":     models.LHTestPeak,
		"symptom_ids": []uint{},
		"notes":       "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-23", bytes.NewReader(body))
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

	day, err := services.ParseDayDate("2026-02-23", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if entry.LHTest != models.LHTestPeak {
		t.Fatalf("expected stored lh test %q, got %q", models.LHTestPeak, entry.LHTest)
	}
}
