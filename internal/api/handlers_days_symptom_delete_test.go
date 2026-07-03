package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestDeleteSymptomArchivesAndKeepsIDsInLogs(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "hide-symptom-history@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	symptom := models.SymptomType{
		UserID: user.ID,
		Name:   "Custom",
		Icon:   "A",
		Color:  "#111111",
	}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create symptom: %v", err)
	}

	logEntry := models.DailyLog{
		UserID:     user.ID,
		Date:       time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC),
		IsPeriod:   false,
		Flow:       models.FlowNone,
		SymptomIDs: []uint{symptom.ID},
		Notes:      "",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/symptoms/"+strconv.FormatUint(uint64(symptom.ID), 10), nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("hide symptom request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("expected status 200, got %d: %s", response.StatusCode, string(body))
	}

	stored := models.SymptomType{}
	if err := database.First(&stored, symptom.ID).Error; err != nil {
		t.Fatalf("load stored symptom: %v", err)
	}
	if stored.ArchivedAt == nil {
		t.Fatalf("expected symptom to be archived, got %#v", stored)
	}

	updated := models.DailyLog{}
	if err := database.First(&updated, logEntry.ID).Error; err != nil {
		t.Fatalf("load updated log: %v", err)
	}
	if len(updated.SymptomIDs) != 1 || updated.SymptomIDs[0] != symptom.ID {
		t.Fatalf("expected symptom IDs to remain in log history, got %#v", updated.SymptomIDs)
	}
}
