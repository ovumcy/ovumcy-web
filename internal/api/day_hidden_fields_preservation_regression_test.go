package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
		BBT:             new(36.65),
		CervicalMucus:   models.CervicalMucusEggWhite,
		CycleFactorKeys: []string{models.CycleFactorStress},
		Notes:           "keep me",
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatalf("create hidden-field log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	todayRaw := today.Format("2006-01-02")
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+todayRaw, strings.NewReader("mood=4"))
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
	if saved.BBT == nil || *saved.BBT != 36.65 {
		t.Fatalf("expected hidden BBT to be preserved, got %v", saved.BBT)
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

// TestDashboardFormSaveClearsEveryTrackedFieldToItsRawStoredZeroValue is the
// complement of TestDashboardFormSavePreservesHiddenOwnerOnlyFields: with every
// field TRACKED (not hidden, so no Preserve* flag applies) and a save that omits
// them, the owner's explicit "nothing here" must reach the raw stored row, not
// just the rendered page or the export payload. export_regressions_test.go
// checks these fields on the export DTO and the preservation test above checks
// the DB only for the hidden/preserved path — neither proves the tracked/cleared
// path ever reaches the raw column, so a clear that only updates the HTML
// response (or a Preserve flag wrongly defaulting true) would ship undetected.
// This reads every field back with fetchLogByDateForTest, the same raw-row
// helper the preservation test uses, so presentation and export stay out of the
// loop entirely.
func TestDashboardFormSaveClearsEveryTrackedFieldToItsRawStoredZeroValue(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "clearable-fields-raw@example.com", "StrongPass1", true)
	// Explicit tracked=true (the zero value already means "not hidden", but
	// state it so a future default flip cannot silently turn this into the
	// preserved path the sibling test already covers).
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"hide_sex_chip":        false,
		"hide_cycle_factors":   false,
		"hide_notes_field":     false,
		"track_bbt":            true,
		"track_cervical_mucus": true,
	}).Error; err != nil {
		t.Fatalf("configure tracked fields: %v", err)
	}

	symptom := models.SymptomType{UserID: user.ID, Name: "Cramps", Icon: "🩸", Color: "#FF4444", IsBuiltin: true}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create symptom: %v", err)
	}

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	entry := models.DailyLog{
		UserID:          user.ID,
		Date:            today,
		IsPeriod:        false,
		Flow:            models.FlowLight,
		Mood:            3,
		SexActivity:     models.SexActivityProtected,
		BBT:             new(36.65),
		CervicalMucus:   models.CervicalMucusEggWhite,
		PregnancyTest:   models.PregnancyTestPositive,
		CycleFactorKeys: []string{models.CycleFactorStress},
		Notes:           "clear me",
		SymptomIDs:      []uint{symptom.ID},
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatalf("create day log to be cleared: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	todayRaw := today.Format("2006-01-02")
	// The full day form, submitted with every clearable field left at its
	// empty/zero wire value and no Preserve* flag set — the "owner cleared
	// everything" shape, not a partial PATCH.
	form := url.Values{
		"is_period":      {"false"},
		"flow":           {models.FlowNone},
		"mood":           {"0"},
		"sex_activity":   {""},
		"cervical_mucus": {""},
		"pregnancy_test": {models.PregnancyTestNone},
		"notes":          {""},
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+todayRaw, strings.NewReader(form.Encode()))
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
	if saved.Flow != models.FlowNone {
		t.Fatalf("expected stored flow cleared to %q, got %q", models.FlowNone, saved.Flow)
	}
	if saved.Mood != 0 {
		t.Fatalf("expected stored mood cleared to 0, got %d", saved.Mood)
	}
	if saved.SexActivity != models.SexActivityNone {
		t.Fatalf("expected stored sex activity cleared to %q, got %q", models.SexActivityNone, saved.SexActivity)
	}
	if saved.BBT != nil {
		t.Fatalf("expected stored BBT cleared to nil (not measured), got %v", *saved.BBT)
	}
	if saved.CervicalMucus != models.CervicalMucusNone {
		t.Fatalf("expected stored cervical mucus cleared to %q, got %q", models.CervicalMucusNone, saved.CervicalMucus)
	}
	if saved.PregnancyTest != models.PregnancyTestNone {
		t.Fatalf("expected stored pregnancy test cleared to %q, got %q", models.PregnancyTestNone, saved.PregnancyTest)
	}
	if len(saved.CycleFactorKeys) != 0 {
		t.Fatalf("expected stored cycle factors cleared, got %#v", saved.CycleFactorKeys)
	}
	if saved.Notes != "" {
		t.Fatalf("expected stored notes cleared, got %q", saved.Notes)
	}
	if len(saved.SymptomIDs) != 0 {
		t.Fatalf("expected stored symptom IDs cleared, got %#v", saved.SymptomIDs)
	}
}
