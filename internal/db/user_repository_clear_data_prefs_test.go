package db

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestClearAllDataRollsBackWipeAndVersionBumpTogether proves the word "atomically"
// in the clear-data claim: the wipe and the auth_session_version bump share one
// transaction, so a failure part-way leaves BOTH undone.
//
// The claim was previously backed only by a test showing the bump happens on
// success, which says nothing about the pair being all-or-nothing. The failure here
// is injected by dropping symptom_types, so the transaction's second statement
// errors after the day-log delete has already run. A non-transactional
// implementation would leave the account in the worst possible state: health data
// gone, version untouched, so every other device keeps its session on an account
// the owner just tried to panic-wipe.
func TestClearAllDataRollsBackWipeAndVersionBumpTogether(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "clear-data-atomic@example.com")

	logDate := time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC)
	if err := repo.database.Create(&models.DailyLog{UserID: user.ID, Date: logDate, IsPeriod: true}).Error; err != nil {
		t.Fatalf("seed day log: %v", err)
	}

	var before models.User
	if err := repo.database.First(&before, user.ID).Error; err != nil {
		t.Fatalf("load user before the wipe: %v", err)
	}

	// Break the statement that runs between the day-log delete and the user update.
	if err := repo.database.Exec("DROP TABLE symptom_types").Error; err != nil {
		t.Fatalf("drop symptom_types: %v", err)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err == nil {
		t.Fatal("expected clear-data to fail once symptom_types is gone")
	}

	var after models.User
	if err := repo.database.First(&after, user.ID).Error; err != nil {
		t.Fatalf("load user after the failed wipe: %v", err)
	}
	if after.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("auth_session_version advanced despite a failed wipe: before=%d after=%d",
			before.AuthSessionVersion, after.AuthSessionVersion)
	}

	var logCount int64
	if err := repo.database.Model(&models.DailyLog{}).Where("user_id = ?", user.ID).Count(&logCount).Error; err != nil {
		t.Fatalf("count day logs after the failed wipe: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("expected the day-log delete to roll back with the rest of the transaction, got %d rows", logCount)
	}
}

// TestClearAllDataResetsDisplayPreferences pins that clear-data returns the
// display/tracking preference toggles to their defaults. show_historical_phases
// was previously loaded by LoadSettingsByID but missing from the reset map, so a
// clear-data wipe left it stuck on — this guards that whole preference group.
func TestClearAllDataResetsDisplayPreferences(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "clear-prefs@example.com")

	if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"track_bbt":              true,
		"track_cervical_mucus":   true,
		"hide_sex_chip":          true,
		"hide_cycle_factors":     true,
		"hide_notes_field":       true,
		"show_historical_phases": true,
	}).Error; err != nil {
		t.Fatalf("seed preferences: %v", err)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	got := reloadUserForWebhook(t, repo, user.ID)
	if got.TrackBBT || got.TrackCervicalMucus || got.HideSexChip || got.HideCycleFactors || got.HideNotesField {
		t.Fatalf("expected tracking toggles reset to false, got %+v", got)
	}
	if got.ShowHistoricalPhases {
		t.Fatal("expected show_historical_phases reset to false after clear-data")
	}
}
