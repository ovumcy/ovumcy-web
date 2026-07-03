package api

// symptoms_idor_regression_test.go pins the handler-level owner-scoping
// invariant for every symptom mutation route. There is no live IDOR — all
// service methods call ...ForUser(user.ID, id) — but without a regression
// test, a future refactor could silently break that scope, turning the
// mutation routes into a cross-account write vector on sensitive health data.
//
// Coverage targets:
//   PATCH  /api/v1/symptoms/:id        — UpdateSymptom
//   DELETE /api/v1/symptoms/:id        — DeleteSymptom (archive)
//   POST   /api/v1/symptoms/:id/restore — RestoreSymptom
//   PUT    /api/v1/days/:date           — UpsertDay with a foreign symptom_id

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestUpdateSymptomByOtherUserReturnsNotFound asserts that PATCH
// /api/v1/symptoms/:id by user A on a symptom owned by user B returns 404
// and leaves B's symptom row unchanged.
func TestUpdateSymptomByOtherUserReturnsNotFound(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	userA := createOnboardingTestUser(t, database, "idor-update-a@example.com", "StrongPass1", true)
	userB := createOnboardingTestUser(t, database, "idor-update-b@example.com", "StrongPass1", true)

	symptomB := models.SymptomType{
		UserID: userB.ID,
		Name:   "B-Symptom",
		Icon:   "🩸",
		Color:  "#AA1122",
	}
	if err := database.Create(&symptomB).Error; err != nil {
		t.Fatalf("seed user B symptom: %v", err)
	}

	authCookieA := loginAndExtractAuthCookie(t, app, userA.Email, "StrongPass1")

	body, err := json.Marshal(map[string]string{
		"name":  "HijackedName",
		"icon":  "🔴",
		"color": "#FF0000",
	})
	if err != nil {
		t.Fatalf("marshal patch body: %v", err)
	}
	path := "/api/v1/symptoms/" + strconv.FormatUint(uint64(symptomB.ID), 10)
	request := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookieA)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user PATCH, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom not found" {
		t.Fatalf("expected symptom not found error key, got %q", got)
	}

	var persisted models.SymptomType
	if err := database.First(&persisted, symptomB.ID).Error; err != nil {
		t.Fatalf("reload symptom B: %v", err)
	}
	if persisted.Name != "B-Symptom" {
		t.Fatalf("cross-user PATCH mutated B's symptom name: got %q, want %q", persisted.Name, "B-Symptom")
	}
}

// TestDeleteSymptomByOtherUserReturnsNotFound asserts that DELETE
// /api/v1/symptoms/:id by user A on a symptom owned by user B returns 404
// and leaves B's symptom unarchived.
func TestDeleteSymptomByOtherUserReturnsNotFound(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	userA := createOnboardingTestUser(t, database, "idor-delete-a@example.com", "StrongPass1", true)
	userB := createOnboardingTestUser(t, database, "idor-delete-b@example.com", "StrongPass1", true)

	symptomB := models.SymptomType{
		UserID: userB.ID,
		Name:   "B-ToBeKept",
		Icon:   "🎈",
		Color:  "#3344FF",
	}
	if err := database.Create(&symptomB).Error; err != nil {
		t.Fatalf("seed user B symptom: %v", err)
	}

	authCookieA := loginAndExtractAuthCookie(t, app, userA.Email, "StrongPass1")

	path := "/api/v1/symptoms/" + strconv.FormatUint(uint64(symptomB.ID), 10)
	request := httptest.NewRequest(http.MethodDelete, path, nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookieA)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user DELETE, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom not found" {
		t.Fatalf("expected symptom not found error key, got %q", got)
	}

	var persisted models.SymptomType
	if err := database.First(&persisted, symptomB.ID).Error; err != nil {
		t.Fatalf("reload symptom B: %v", err)
	}
	if persisted.ArchivedAt != nil {
		t.Fatalf("cross-user DELETE archived B's symptom; ArchivedAt should be nil")
	}
}

// TestRestoreSymptomByOtherUserReturnsNotFound asserts that POST
// /api/v1/symptoms/:id/restore by user A on an archived symptom owned by
// user B returns 404 and leaves B's symptom archived.
func TestRestoreSymptomByOtherUserReturnsNotFound(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	userA := createOnboardingTestUser(t, database, "idor-restore-a@example.com", "StrongPass1", true)
	userB := createOnboardingTestUser(t, database, "idor-restore-b@example.com", "StrongPass1", true)

	archivedAt := time.Now().UTC()
	symptomB := models.SymptomType{
		UserID:     userB.ID,
		Name:       "B-Archived",
		Icon:       "🌙",
		Color:      "#556677",
		ArchivedAt: &archivedAt,
	}
	if err := database.Create(&symptomB).Error; err != nil {
		t.Fatalf("seed archived user B symptom: %v", err)
	}

	authCookieA := loginAndExtractAuthCookie(t, app, userA.Email, "StrongPass1")

	path := "/api/v1/symptoms/" + strconv.FormatUint(uint64(symptomB.ID), 10) + "/restore"
	request := httptest.NewRequest(http.MethodPost, path, nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookieA)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user POST restore, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom not found" {
		t.Fatalf("expected symptom not found error key, got %q", got)
	}

	var persisted models.SymptomType
	if err := database.First(&persisted, symptomB.ID).Error; err != nil {
		t.Fatalf("reload symptom B: %v", err)
	}
	if persisted.ArchivedAt == nil {
		t.Fatalf("cross-user POST restore cleared ArchivedAt on B's symptom; it should remain archived")
	}
}

// TestUpsertDayRejectsSymptomIDOwnedByOtherUser asserts that PUT
// /api/v1/days/:date by user A carrying a symptom_id that belongs to user B
// is rejected with 400, not silently accepted or persisted.
//
// ValidateSymptomIDs calls CountByUserAndIDs scoped to user A, so B's ID
// counts as "not found" for A and the handler returns an error before
// writing any log row.
func TestUpsertDayRejectsSymptomIDOwnedByOtherUser(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	userA := createOnboardingTestUser(t, database, "idor-day-a@example.com", "StrongPass1", true)
	userB := createOnboardingTestUser(t, database, "idor-day-b@example.com", "StrongPass1", true)

	symptomB := models.SymptomType{
		UserID: userB.ID,
		Name:   "B-Only",
		Icon:   "🍫",
		Color:  "#8B4513",
	}
	if err := database.Create(&symptomB).Error; err != nil {
		t.Fatalf("seed user B symptom: %v", err)
	}

	authCookieA := loginAndExtractAuthCookie(t, app, userA.Email, "StrongPass1")

	payload := map[string]any{
		"is_period":   false,
		"flow":        models.FlowNone,
		"symptom_ids": []uint{symptomB.ID},
		"notes":       "",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-03-15", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookieA)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-user symptom_id in day upsert, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid symptom ids" {
		t.Fatalf("expected invalid symptom ids error key, got %q", got)
	}

	// No log row must have been created for user A on that date.
	var rowCount int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ?", userA.ID).Count(&rowCount).Error; err != nil {
		t.Fatalf("count user A log rows: %v", err)
	}
	if rowCount != 0 {
		t.Fatalf("day upsert with foreign symptom_id created %d log row(s) for user A; expected 0", rowCount)
	}
}
