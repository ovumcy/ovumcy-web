package db

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

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
