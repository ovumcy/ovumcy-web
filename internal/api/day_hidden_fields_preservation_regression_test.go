package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestDashboardFormSavePreservesHiddenOwnerOnlyFields(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "hidden-fields-preserve@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"hide_sex_chip":        true,
		"hide_cycle_factors":   true,
		"hide_notes_field":     true,
		"track_bbt":            false,
		"track_cervical_mucus": false,
	}).Error; err != nil {
		t.Fatalf("configure hidden tracking fields: %v", err)
	}

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	entry := models.DailyLog{
		UserID:          user.ID,
		Date:            today,
		Mood:            2,
		SexActivity:     models.SexActivityProtected,
		BBT:             36.65,
		CervicalMucus:   models.CervicalMucusEggWhite,
		CycleFactorKeys: []string{models.CycleFactorStress},
		Notes:           "keep me",
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatalf("create hidden-field log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	todayRaw := today.Format("2006-01-02")
	request := httptest.NewRequest(http.MethodPost, "/api/days/"+todayRaw, strings.NewReader("mood=4"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	saved, err := fetchLogByDateForTest(database, user.ID, today, time.UTC)
	if err != nil {
		t.Fatalf("load saved log: %v", err)
	}
	if saved.Mood != 4 {
		t.Fatalf("expected updated mood 4, got %d", saved.Mood)
	}
	if saved.SexActivity != models.SexActivityProtected {
		t.Fatalf("expected hidden sex activity to be preserved, got %q", saved.SexActivity)
	}
	if saved.BBT != 36.65 {
		t.Fatalf("expected hidden BBT to be preserved, got %.2f", saved.BBT)
	}
	if saved.CervicalMucus != models.CervicalMucusEggWhite {
		t.Fatalf("expected hidden cervical mucus to be preserved, got %q", saved.CervicalMucus)
	}
	if len(saved.CycleFactorKeys) != 1 || saved.CycleFactorKeys[0] != models.CycleFactorStress {
		t.Fatalf("expected hidden cycle factors to be preserved, got %#v", saved.CycleFactorKeys)
	}
	if saved.Notes != "keep me" {
		t.Fatalf("expected hidden notes to be preserved, got %q", saved.Notes)
	}
}
