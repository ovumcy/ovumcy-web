package db

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestRevokeRestoredAccessReachesEveryAccount pins the restore fence's second
// bulk write (WEB-399, WEB-408) on both dialects: every account's session
// version moves, every stored endpoint is cleared with its delivery mark and an
// advanced revocation epoch, every local password is revoked into the
// OIDC-only shape, and the owners' webhook preferences stay.
func TestRevokeRestoredAccessReachesEveryAccount(t *testing.T) {
	dialects := map[string]func(t *testing.T) *UserRepository{
		"sqlite": openWebhookRepoForTest,
		"postgres": func(t *testing.T) *UserRepository {
			return NewUserRepository(openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t)))
		},
	}
	for name, open := range dialects {
		t.Run(name, func(t *testing.T) {
			repo := open(t)
			ctx := context.Background()
			withHook := createUserForTimezoneTest(t, repo, "restore-revoke-hook@example.com")
			without := createUserForTimezoneTest(t, repo, "restore-revoke-plain@example.com")
			oidcOnly := createUserForTimezoneTest(t, repo, "restore-revoke-oidc@example.com")
			if err := repo.database.Model(&models.User{}).Where("id = ?", withHook.ID).Updates(map[string]any{
				"webhook_enabled":          true,
				"webhook_url":              "sealed-endpoint",
				"webhook_notify_ovulation": false,
				"recovery_code_hash":       "recovery",
			}).Error; err != nil {
				t.Fatalf("store a webhook: %v", err)
			}
			if err := repo.database.Model(&models.User{}).Where("id = ?", oidcOnly.ID).
				Updates(map[string]any{"password_hash": "", "local_auth_enabled": false}).Error; err != nil {
				t.Fatalf("make an OIDC-only account: %v", err)
			}
			beforeHook := reloadUserForWebhookRevocationTest(t, repo, withHook.ID)
			beforePlain := reloadUserForWebhookRevocationTest(t, repo, without.ID)

			counts, err := repo.RevokeRestoredAccess(ctx)
			if err != nil {
				t.Fatalf("RevokeRestoredAccess: %v", err)
			}
			if counts != (models.RestoreRevocationCounts{Sessions: 3, WebhookEndpoints: 1, LocalPasswords: 2}) {
				t.Fatalf("expected 3 sessions, 1 endpoint and 2 passwords revoked, got %+v", counts)
			}

			afterHook := reloadUserForWebhookRevocationTest(t, repo, withHook.ID)
			afterPlain := reloadUserForWebhookRevocationTest(t, repo, without.ID)
			if afterHook.AuthSessionVersion != beforeHook.AuthSessionVersion+1 || afterPlain.AuthSessionVersion != beforePlain.AuthSessionVersion+1 {
				t.Fatalf("every account's session version must move: hook %d→%d, plain %d→%d",
					beforeHook.AuthSessionVersion, afterHook.AuthSessionVersion, beforePlain.AuthSessionVersion, afterPlain.AuthSessionVersion)
			}
			for _, user := range []models.User{afterHook, afterPlain} {
				if user.PasswordHash != "" || user.RecoveryCodeHash != "" || user.LocalAuthEnabled {
					t.Fatalf("account %d keeps a restored credential: hash %q, recovery %q, local %t", user.ID, user.PasswordHash, user.RecoveryCodeHash, user.LocalAuthEnabled)
				}
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
		})
	}
}

// TestRevokeRestoredAccessWritesNothingWhenAnyStatementFails pins the
// one-transaction claim: a refused password revocation, session bump or
// endpoint clear rolls back every other statement, so the boot retries all of
// them rather than recording a restore it only partly answered.
func TestRevokeRestoredAccessWritesNothingWhenAnyStatementFails(t *testing.T) {
	for _, column := range []string{"password_hash", "auth_session_version", "webhook_url"} {
		t.Run(column, func(t *testing.T) {
			repo := openWebhookRepoForTest(t)
			user := createUserForTimezoneTest(t, repo, "restore-revoke-refused@example.com")
			if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).
				Updates(map[string]any{"webhook_enabled": true, "webhook_url": "sealed-endpoint"}).Error; err != nil {
				t.Fatalf("store a webhook: %v", err)
			}
			// An OIDC-only account too, so the session bump that runs first —
			// over accounts with no password — has a row to be refused on.
			oidcOnly := createUserForTimezoneTest(t, repo, "restore-revoke-refused-oidc@example.com")
			if err := repo.database.Model(&models.User{}).Where("id = ?", oidcOnly.ID).
				Updates(map[string]any{"password_hash": "", "local_auth_enabled": false}).Error; err != nil {
				t.Fatalf("make an OIDC-only account: %v", err)
			}
			before := reloadUserForWebhookRevocationTest(t, repo, user.ID)
			if err := repo.database.Exec(`CREATE TRIGGER refuse_` + column + ` BEFORE UPDATE OF ` + column +
				` ON users BEGIN SELECT RAISE(ABORT, 'refused'); END`).Error; err != nil {
				t.Fatalf("create the trigger: %v", err)
			}

			counts, err := repo.RevokeRestoredAccess(context.Background())
			if err == nil {
				t.Fatal("a refused statement must surface as an error")
			}
			if counts != (models.RestoreRevocationCounts{}) {
				t.Fatalf("a failed revocation must report no counts, got %+v", counts)
			}
			after := reloadUserForWebhookRevocationTest(t, repo, user.ID)
			if after.AuthSessionVersion != before.AuthSessionVersion || after.WebhookURL != before.WebhookURL ||
				after.PasswordHash != before.PasswordHash || after.LocalAuthEnabled != before.LocalAuthEnabled {
				t.Fatalf("a failed revocation must leave the row as it was: before %+v, after %+v", before, after)
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
