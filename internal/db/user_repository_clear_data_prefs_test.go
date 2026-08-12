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

// TestClearAllDataPreservesInterfaceLanguage pins users.interface_language OUT
// of the clear-data reset map (migration 034). Its sibling above pins timezone
// INTO that map, and the pair is the whole distinction: timezone is a coarse
// location signal inferred from the browser, while the interface language is
// the language the owner reads the product in — resetting it would answer a
// "wipe my records" gesture by switching the UI back to English mid-session,
// and would do so on every device at once through the next sign-in.
//
// The reset half is the positive anchor: asserting only "the language survived"
// would pass just as well against a clear-data that reset nothing at all, so
// the same test checks that a preference which IS owner data (timezone) went
// back to its default in the same call.
func TestClearAllDataPreservesInterfaceLanguage(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "clear-interface-language@example.com")

	if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"interface_language": "ru",
	}).Error; err != nil {
		t.Fatalf("seed interface language: %v", err)
	}
	if err := repo.UpdateUserTimezone(context.Background(), user.ID, "Europe/Belgrade"); err != nil {
		t.Fatalf("seed timezone: %v", err)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	got := reloadUserForWebhook(t, repo, user.ID)
	if got.InterfaceLanguage != "ru" {
		t.Fatalf("expected interface_language preserved through clear-data, got %q", got.InterfaceLanguage)
	}
	if got.Timezone != "" {
		t.Fatalf("expected timezone reset by the same clear-data call, got %q", got.Timezone)
	}
}

// TestClearAllDataResetsTimezoneAndKeepsIdentity pins users.timezone into the
// clear-data reset map. The column is written from the request (the owner's
// browser-detected IANA zone) and read by the request-free reminder pass, so it
// is a coarse location signal — and it was the one preference a wipe left
// standing while every other preference returned to its default.
//
// The identity half is the positive anchor: asserting only "timezone is empty"
// would pass just as well if clear-data blanked the whole row, so the same test
// checks that email, password hash, and display name survive the wipe.
func TestClearAllDataResetsTimezoneAndKeepsIdentity(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "clear-timezone@example.com")

	const displayName = "Owner Persona"
	if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"display_name": displayName,
	}).Error; err != nil {
		t.Fatalf("seed display name: %v", err)
	}
	if err := repo.UpdateUserTimezone(context.Background(), user.ID, "Europe/Belgrade"); err != nil {
		t.Fatalf("seed timezone: %v", err)
	}

	before := reloadUserForWebhook(t, repo, user.ID)
	if before.Timezone != "Europe/Belgrade" {
		t.Fatalf("expected seeded timezone Europe/Belgrade, got %q", before.Timezone)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	got := reloadUserForWebhook(t, repo, user.ID)
	if got.Timezone != "" {
		t.Fatalf("expected timezone reset to its column default (empty) after clear-data, got %q", got.Timezone)
	}
	for _, check := range []struct {
		name          string
		before, after string
	}{
		{"email", before.Email, got.Email},
		{"password_hash", before.PasswordHash, got.PasswordHash},
		{"recovery_code_hash", before.RecoveryCodeHash, got.RecoveryCodeHash},
		{"display_name", displayName, got.DisplayName},
	} {
		if check.before != check.after {
			t.Fatalf("expected %s preserved through clear-data, before=%q after=%q", check.name, check.before, check.after)
		}
	}
}
