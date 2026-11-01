package db

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestRevokeAllSessionsAndWebhookEndpointsReachesEveryAccount pins the restore
// fence's second bulk write (WEB-399): every account's session version moves,
// every stored endpoint is cleared with its delivery mark and an advanced
// revocation epoch, and the owners' webhook preferences stay.
func TestRevokeAllSessionsAndWebhookEndpointsReachesEveryAccount(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	ctx := context.Background()
	withHook := createUserForTimezoneTest(t, repo, "restore-revoke-hook@example.com")
	without := createUserForTimezoneTest(t, repo, "restore-revoke-plain@example.com")
	if err := repo.database.Model(&models.User{}).Where("id = ?", withHook.ID).Updates(map[string]any{
		"webhook_enabled":          true,
		"webhook_url":              "sealed-endpoint",
		"webhook_notify_ovulation": false,
	}).Error; err != nil {
		t.Fatalf("store a webhook: %v", err)
	}
	beforeHook := reloadUserForWebhookRevocationTest(t, repo, withHook.ID)
	beforePlain := reloadUserForWebhookRevocationTest(t, repo, without.ID)

	counts, err := repo.RevokeAllSessionsAndWebhookEndpoints(ctx)
	if err != nil {
		t.Fatalf("RevokeAllSessionsAndWebhookEndpoints: %v", err)
	}
	if counts != (models.RestoreRevocationCounts{Sessions: 2, WebhookEndpoints: 1}) {
		t.Fatalf("expected 2 sessions and 1 endpoint revoked, got %+v", counts)
	}

	afterHook := reloadUserForWebhookRevocationTest(t, repo, withHook.ID)
	afterPlain := reloadUserForWebhookRevocationTest(t, repo, without.ID)
	if afterHook.AuthSessionVersion != beforeHook.AuthSessionVersion+1 || afterPlain.AuthSessionVersion != beforePlain.AuthSessionVersion+1 {
		t.Fatalf("every account's session version must move: hook %d→%d, plain %d→%d",
			beforeHook.AuthSessionVersion, afterHook.AuthSessionVersion, beforePlain.AuthSessionVersion, afterPlain.AuthSessionVersion)
	}
	if afterHook.WebhookEnabled || afterHook.WebhookURL != "" || afterHook.WebhookLastDeliveredAt != nil {
		t.Fatalf("the endpoint must be cleared, got %+v", afterHook)
	}
	if afterHook.WebhookConfigVersion != beforeHook.WebhookConfigVersion+1 {
		t.Fatalf("the revocation epoch must advance: %d→%d", beforeHook.WebhookConfigVersion, afterHook.WebhookConfigVersion)
	}
	if afterHook.WebhookNotifyOvulation {
		t.Fatal("the owner's per-kind opt-in must survive")
	}
	if afterPlain.WebhookConfigVersion != beforePlain.WebhookConfigVersion {
		t.Fatal("an account with no endpoint must not have its webhook epoch moved")
	}
}

// TestRevokeAllSessionsAndWebhookEndpointsWritesNothingWhenEitherHalfFails pins
// the one-transaction claim: a refused session bump reports the error, and a
// refused endpoint clear also rolls the session bump back, so the boot retries
// both rather than recording a restore it only half answered.
func TestRevokeAllSessionsAndWebhookEndpointsWritesNothingWhenEitherHalfFails(t *testing.T) {
	for _, column := range []string{"auth_session_version", "webhook_url"} {
		t.Run(column, func(t *testing.T) {
			repo := openWebhookRepoForTest(t)
			user := createUserForTimezoneTest(t, repo, "restore-revoke-refused@example.com")
			if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).
				Updates(map[string]any{"webhook_enabled": true, "webhook_url": "sealed-endpoint"}).Error; err != nil {
				t.Fatalf("store a webhook: %v", err)
			}
			before := reloadUserForWebhookRevocationTest(t, repo, user.ID)
			if err := repo.database.Exec(`CREATE TRIGGER refuse_` + column + ` BEFORE UPDATE OF ` + column +
				` ON users BEGIN SELECT RAISE(ABORT, 'refused'); END`).Error; err != nil {
				t.Fatalf("create the trigger: %v", err)
			}

			counts, err := repo.RevokeAllSessionsAndWebhookEndpoints(context.Background())
			if err == nil {
				t.Fatal("a refused statement must surface as an error")
			}
			if counts != (models.RestoreRevocationCounts{}) {
				t.Fatalf("a failed revocation must report no counts, got %+v", counts)
			}
			after := reloadUserForWebhookRevocationTest(t, repo, user.ID)
			if after.AuthSessionVersion != before.AuthSessionVersion || after.WebhookURL != before.WebhookURL {
				t.Fatalf("a failed revocation must leave the row as it was: version %d→%d, url %q→%q",
					before.AuthSessionVersion, after.AuthSessionVersion, before.WebhookURL, after.WebhookURL)
			}
		})
	}
}

func reloadUserForWebhookRevocationTest(t *testing.T, repo *UserRepository, userID uint) models.User {
	t.Helper()
	var user models.User
	if err := repo.database.First(&user, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return user
}
