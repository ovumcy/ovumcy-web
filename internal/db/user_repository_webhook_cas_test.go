package db

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestSaveWebhookSettingsRefusesARowAnotherWriterMovedOn is the remove-versus-
// re-save race. A settings form decided "keep the stored endpoint" from a read
// taken before the owner removed it elsewhere; re-storing that endpoint after
// the removal would re-arm a destination the owner was told is gone. The save
// is pinned to the epoch its read held, and the removal advanced it.
func TestSaveWebhookSettingsRefusesARowAnotherWriterMovedOn(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-cas-remove@example.com")
	ctx := context.Background()

	if err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "sealed-endpoint",
		NotifyPeriod:     true,
		ReminderLeadDays: 3,
	}); err != nil {
		t.Fatalf("seed webhook settings: %v", err)
	}
	readEpoch := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion

	if err := repo.RemoveWebhookDestination(ctx, user.ID); err != nil {
		t.Fatalf("RemoveWebhookDestination: %v", err)
	}

	err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
		Enabled:               true,
		EncryptedURL:          "sealed-endpoint",
		NotifyPeriod:          true,
		ReminderLeadDays:      3,
		ExpectedConfigVersion: readEpoch,
	})
	if !errors.Is(err, models.ErrWebhookConfigVersionChanged) {
		t.Fatalf("expected the stale save refused, got %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookURL != "" || after.WebhookEnabled {
		t.Fatalf("a save decided before the removal re-armed the endpoint: url=%q enabled=%t", after.WebhookURL, after.WebhookEnabled)
	}

	// The same save pinned to the row as it now stands lands.
	if err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
		Enabled:               true,
		EncryptedURL:          "sealed-endpoint-2",
		NotifyPeriod:          true,
		ReminderLeadDays:      3,
		ExpectedConfigVersion: after.WebhookConfigVersion,
	}); err != nil {
		t.Fatalf("a save pinned to the current epoch: %v", err)
	}
}

// TestSaveWebhookSettingsSurfacesAWriteFailureAsItself: a failed UPDATE is a
// storage error, never reported as a lost pin the caller would retry.
func TestSaveWebhookSettingsSurfacesAWriteFailureAsItself(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-cas-write-fails@example.com")
	sqlDB, err := repo.database.DB()
	if err != nil {
		t.Fatalf("database handle: %v", err)
	}
	_ = sqlDB.Close()

	err = repo.SaveWebhookSettings(context.Background(), user.ID, models.WebhookSettingsColumns{ReminderLeadDays: 3})
	if err == nil || errors.Is(err, models.ErrWebhookConfigVersionChanged) {
		t.Fatalf("expected the storage error itself, got %v", err)
	}
}

// TestSaveWebhookSettingsRefusesAMissingOwner keeps the pin from reading a
// zero-row UPDATE as success.
func TestSaveWebhookSettingsRefusesAMissingOwner(t *testing.T) {
	repo := openWebhookRepoForTest(t)

	err := repo.SaveWebhookSettings(context.Background(), 987654, models.WebhookSettingsColumns{ReminderLeadDays: 3})
	if !errors.Is(err, models.ErrWebhookConfigVersionChanged) {
		t.Fatalf("expected a save against no row refused, got %v", err)
	}
}
