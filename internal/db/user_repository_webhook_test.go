package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func openWebhookRepoForTest(t *testing.T) *UserRepository {
	t.Helper()
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "webhook.db"))
	return NewUserRepository(database)
}

func reloadUserForWebhook(t *testing.T, repo *UserRepository, userID uint) models.User {
	t.Helper()
	var reloaded models.User
	if err := repo.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded
}

// TestSaveWebhookSettingsPersistsColumns proves the narrow write stores exactly
// the settings columns handed to it (webhook_url as the opaque ciphertext the
// service produced) and leaves auth_session_version untouched — a notification
// preference change is not a security-posture change.
func TestSaveWebhookSettingsPersistsColumns(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-persist@example.com")

	before := reloadUserForWebhook(t, repo, user.ID)
	if before.WebhookEnabled {
		t.Fatal("expected webhook_enabled=false on a fresh user")
	}
	if before.ReminderLeadDays != models.DefaultReminderLeadDays {
		t.Fatalf("expected fresh reminder_lead_days=%d, got %d", models.DefaultReminderLeadDays, before.ReminderLeadDays)
	}

	const opaqueCiphertext = "opaque-ciphertext-stand-in"
	if err := repo.SaveWebhookSettings(context.Background(), user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     opaqueCiphertext,
		NotifyPeriod:     true,
		NotifyOvulation:  false,
		ReminderLeadDays: 7,
	}); err != nil {
		t.Fatalf("SaveWebhookSettings: %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if !after.WebhookEnabled {
		t.Fatal("expected webhook_enabled=true after save")
	}
	if after.WebhookURL != opaqueCiphertext {
		t.Fatalf("expected webhook_url to store the ciphertext verbatim, got %q", after.WebhookURL)
	}
	if !after.WebhookNotifyPeriod {
		t.Fatal("expected webhook_notify_period=true after save")
	}
	if after.WebhookNotifyOvulation {
		t.Fatal("expected webhook_notify_ovulation=false after save")
	}
	if after.ReminderLeadDays != 7 {
		t.Fatalf("expected reminder_lead_days=7 after save, got %d", after.ReminderLeadDays)
	}
	if after.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("SaveWebhookSettings must not bump auth_session_version: before=%d after=%d", before.AuthSessionVersion, after.AuthSessionVersion)
	}
}

// TestSaveWebhookSettingsScopedToUser proves the write is strictly scoped to the
// target user id: saving owner A's webhook settings never touches owner B's row
// (the household-multi-owner isolation boundary).
func TestSaveWebhookSettingsScopedToUser(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	owner := createUserForTimezoneTest(t, repo, "wh-owner@example.com")
	other := createUserForTimezoneTest(t, repo, "wh-other@example.com")

	if err := repo.SaveWebhookSettings(context.Background(), other.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "other-ciphertext",
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: 5,
	}); err != nil {
		t.Fatalf("seed other owner webhook settings: %v", err)
	}

	if err := repo.SaveWebhookSettings(context.Background(), owner.ID, models.WebhookSettingsColumns{
		Enabled:          false,
		EncryptedURL:     "owner-ciphertext",
		NotifyPeriod:     false,
		NotifyOvulation:  false,
		ReminderLeadDays: 1,
	}); err != nil {
		t.Fatalf("SaveWebhookSettings owner: %v", err)
	}

	gotOther := reloadUserForWebhook(t, repo, other.ID)
	if gotOther.WebhookURL != "other-ciphertext" || !gotOther.WebhookEnabled || gotOther.ReminderLeadDays != 5 {
		t.Fatalf("other owner row was mutated by owner save: %+v", gotOther)
	}
	gotOwner := reloadUserForWebhook(t, repo, owner.ID)
	if gotOwner.WebhookURL != "owner-ciphertext" || gotOwner.WebhookEnabled || gotOwner.ReminderLeadDays != 1 {
		t.Fatalf("owner row not persisted as expected: %+v", gotOwner)
	}
}

// TestListAllForNotifyReturnsWhitelistedColumns proves the notify projection
// returns the webhook settings, watermarks, and cycle inputs for every owner,
// carrying webhook_url as the stored ciphertext.
func TestListAllForNotifyReturnsWhitelistedColumns(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	first := createUserForTimezoneTest(t, repo, "notify-1@example.com")
	second := createUserForTimezoneTest(t, repo, "notify-2@example.com")

	if err := repo.UpdateUserTimezone(context.Background(), first.ID, "Europe/Belgrade"); err != nil {
		t.Fatalf("seed timezone: %v", err)
	}
	if err := repo.SaveWebhookSettings(context.Background(), first.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "cipher-first",
		NotifyPeriod:     true,
		NotifyOvulation:  false,
		ReminderLeadDays: 4,
	}); err != nil {
		t.Fatalf("seed first webhook settings: %v", err)
	}

	// Set a watermark directly to confirm it round-trips through the projection.
	anchor := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	if err := repo.database.Model(&models.User{}).Where("id = ?", first.ID).
		Update("webhook_period_last_sent_cycle_start", anchor).Error; err != nil {
		t.Fatalf("seed watermark: %v", err)
	}

	records, err := repo.ListAllForNotify(context.Background())
	if err != nil {
		t.Fatalf("ListAllForNotify: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 notify records, got %d", len(records))
	}

	byID := make(map[uint]models.WebhookNotifyRecord, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}

	got := byID[first.ID]
	if !got.WebhookEnabled {
		t.Fatal("expected first record webhook_enabled=true")
	}
	if got.WebhookURL != "cipher-first" {
		t.Fatalf("expected first record webhook_url ciphertext 'cipher-first', got %q", got.WebhookURL)
	}
	if !got.WebhookNotifyPeriod || got.WebhookNotifyOvulation {
		t.Fatalf("first record notify flags mismatch: period=%v ovulation=%v", got.WebhookNotifyPeriod, got.WebhookNotifyOvulation)
	}
	if got.ReminderLeadDays != 4 {
		t.Fatalf("expected first record reminder_lead_days=4, got %d", got.ReminderLeadDays)
	}
	if got.Timezone != "Europe/Belgrade" {
		t.Fatalf("expected first record timezone Europe/Belgrade, got %q", got.Timezone)
	}
	if got.CycleLength != 28 || got.PeriodLength != 5 {
		t.Fatalf("expected cycle inputs to load, got cycle=%d period=%d", got.CycleLength, got.PeriodLength)
	}
	if got.WebhookPeriodLastSentCycleStart == nil || !got.WebhookPeriodLastSentCycleStart.Equal(anchor) {
		t.Fatalf("expected first record period watermark %s, got %v", anchor, got.WebhookPeriodLastSentCycleStart)
	}

	// The untouched second owner is present with column defaults and a nil
	// watermark (no reminder ever sent).
	other := byID[second.ID]
	if other.WebhookEnabled {
		t.Fatal("expected second record webhook_enabled=false")
	}
	if other.ReminderLeadDays != models.DefaultReminderLeadDays {
		t.Fatalf("expected second record default reminder_lead_days=%d, got %d", models.DefaultReminderLeadDays, other.ReminderLeadDays)
	}
	if other.WebhookPeriodLastSentCycleStart != nil {
		t.Fatalf("expected second record nil period watermark, got %v", other.WebhookPeriodLastSentCycleStart)
	}
}

// TestClearAllDataResetsWebhookColumns proves a clear-data wipe disarms
// webhook delivery, clears the encrypted endpoint, resets the shared lead
// window and per-kind opt-ins to defaults, and clears both watermarks so no
// stale reminder can fire against the freshly emptied account.
func TestClearAllDataResetsWebhookColumns(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-clear@example.com")

	if err := repo.SaveWebhookSettings(context.Background(), user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "cipher-to-wipe",
		NotifyPeriod:     false,
		NotifyOvulation:  false,
		ReminderLeadDays: 10,
	}); err != nil {
		t.Fatalf("seed webhook settings: %v", err)
	}
	anchor := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"webhook_period_last_sent_cycle_start":    anchor,
		"webhook_ovulation_last_sent_cycle_start": anchor,
	}).Error; err != nil {
		t.Fatalf("seed watermarks: %v", err)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	got := reloadUserForWebhook(t, repo, user.ID)
	if got.WebhookEnabled {
		t.Fatal("expected webhook_enabled=false after clear-data")
	}
	if got.WebhookURL != "" {
		t.Fatalf("expected webhook_url cleared after clear-data, got %q", got.WebhookURL)
	}
	if !got.WebhookNotifyPeriod || !got.WebhookNotifyOvulation {
		t.Fatalf("expected per-kind opt-ins reset to true, got period=%v ovulation=%v", got.WebhookNotifyPeriod, got.WebhookNotifyOvulation)
	}
	if got.ReminderLeadDays != models.DefaultReminderLeadDays {
		t.Fatalf("expected reminder_lead_days reset to %d, got %d", models.DefaultReminderLeadDays, got.ReminderLeadDays)
	}
	if got.WebhookPeriodLastSentCycleStart != nil || got.WebhookOvulationLastSentCycleStart != nil {
		t.Fatalf("expected watermarks cleared after clear-data, got period=%v ovulation=%v", got.WebhookPeriodLastSentCycleStart, got.WebhookOvulationLastSentCycleStart)
	}
}

// TestListAllForNotifyReturnsErrorOnQueryFailure exercises the error-return
// branch of ListAllForNotify: when the underlying SELECT fails (here because the
// users table has been dropped), the method must propagate the error and return
// a nil slice rather than report empty success. Mirrors the drop-table technique
// the account-erasure tests use to reach their own error branches.
func TestListAllForNotifyReturnsErrorOnQueryFailure(t *testing.T) {
	repo := openWebhookRepoForTest(t)

	if err := repo.database.Exec("DROP TABLE users").Error; err != nil {
		t.Fatalf("drop users table: %v", err)
	}

	records, err := repo.ListAllForNotify(context.Background())
	if err == nil {
		t.Fatal("expected ListAllForNotify to error when the users table is missing")
	}
	if records != nil {
		t.Fatalf("expected nil records on error, got %v", records)
	}
}
