package db

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

var restoreAcrossRevocationSecret = []byte("restore-across-revocation-key-01")

// restoreSnapshot is what a backup holds of one account: its row, its linked
// identities, and the database half of the fence.
type restoreSnapshot struct {
	user       models.User
	identities []models.OIDCIdentity
	appState   []models.AppState
}

func takeRestoreSnapshot(t *testing.T, database *gorm.DB, userID uint) restoreSnapshot {
	t.Helper()
	var snapshot restoreSnapshot
	if err := database.First(&snapshot.user, userID).Error; err != nil {
		t.Fatalf("snapshot user: %v", err)
	}
	if err := database.Where("user_id = ?", userID).Find(&snapshot.identities).Error; err != nil {
		t.Fatalf("snapshot identities: %v", err)
	}
	if err := database.Find(&snapshot.appState).Error; err != nil {
		t.Fatalf("snapshot app_state: %v", err)
	}
	return snapshot
}

// restoreRestoreSnapshot puts the snapshot back the way a restore of that
// backup would, with the fence file left where it is.
func restoreRestoreSnapshot(t *testing.T, database *gorm.DB, snapshot restoreSnapshot) {
	t.Helper()
	err := database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&snapshot.user).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", snapshot.user.ID).Delete(&models.OIDCIdentity{}).Error; err != nil {
			return err
		}
		for index := range snapshot.identities {
			if err := tx.Create(&snapshot.identities[index]).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("1 = 1").Delete(&models.AppState{}).Error; err != nil {
			return err
		}
		if len(snapshot.appState) == 0 {
			return nil
		}
		return tx.Create(&snapshot.appState).Error
	})
	if err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}
}

// TestARestoreAcrossASessionOrCredentialRevocationRevokesOnTheNextBoot is
// WEB-418 and WEB-419's regression, on both dialects, over the real fence file
// and app_state: a backup is taken, the owner revokes something — a sign-out, a
// TOTP enable or disable, a password change, an OIDC link or unlink — and the
// backup is restored. Before the fix the fence halves still agreed, the boot
// read continuity, and the restored row accepted the cookie, password or second
// factor the revocation had replaced. The boot must read the restore, and a
// cookie minted at the backup's session version — which the restored row
// resolves again — must stop resolving.
func TestARestoreAcrossASessionOrCredentialRevocationRevokesOnTheNextBoot(t *testing.T) {
	dialects := map[string]func(t *testing.T) *gorm.DB{
		"sqlite": func(t *testing.T) *gorm.DB {
			return openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "restore-across-revocation.db"))
		},
		"postgres": func(t *testing.T) *gorm.DB {
			return openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
		},
	}
	type fixture struct {
		repositories *Repositories
		user         models.User
	}
	revocations := []struct {
		name   string
		setup  func(t *testing.T, f fixture)
		revoke func(ctx context.Context, f fixture) error
	}{
		{
			name: "sign-out",
			revoke: func(ctx context.Context, f fixture) error {
				return f.repositories.Users.BumpAuthSessionVersion(ctx, f.user.ID)
			},
		},
		{
			name: "totp-enable",
			revoke: func(ctx context.Context, f fixture) error {
				return f.repositories.Users.UpdateTOTPFieldsAndRevokeSessions(ctx, f.user.ID, f.user.AuthSessionVersion, "sealed-totp-secret", true, 7)
			},
		},
		{
			name: "totp-disable",
			setup: func(t *testing.T, f fixture) {
				if err := f.repositories.Users.database.Model(&models.User{}).Where("id = ?", f.user.ID).
					Updates(map[string]any{"totp_secret": "sealed-totp-secret", "totp_enabled": true}).Error; err != nil {
					t.Fatalf("enable TOTP: %v", err)
				}
			},
			revoke: func(ctx context.Context, f fixture) error {
				return f.repositories.Users.UpdateTOTPFieldsAndRevokeSessions(ctx, f.user.ID, f.user.AuthSessionVersion, "", false, 0)
			},
		},
		{
			name: "password-change",
			revoke: func(ctx context.Context, f fixture) error {
				return f.repositories.Users.UpdatePasswordAndRevokeSessions(ctx, f.user.ID, f.user.AuthSessionVersion, "replacement-hash", false)
			},
		},
		{
			name: "oidc-link",
			revoke: func(ctx context.Context, f fixture) error {
				identity := models.OIDCIdentity{UserID: f.user.ID, Issuer: "https://issuer.example", Subject: "link-" + f.user.Email}
				return f.repositories.OIDCIdentities.CreateAndRevokeSessions(ctx, &identity, f.user.AuthSessionVersion)
			},
		},
		{
			name: "oidc-unlink",
			setup: func(t *testing.T, f fixture) {
				identity := models.OIDCIdentity{UserID: f.user.ID, Issuer: "https://issuer.example", Subject: "unlink-" + f.user.Email}
				if err := f.repositories.OIDCIdentities.Create(context.Background(), &identity); err != nil {
					t.Fatalf("link identity: %v", err)
				}
			},
			revoke: func(ctx context.Context, f fixture) error {
				identities, err := f.repositories.OIDCIdentities.ListByUser(ctx, f.user.ID)
				if err != nil || len(identities) != 1 {
					return fmt.Errorf("list identities: %d, %v", len(identities), err)
				}
				deleted, err := f.repositories.OIDCIdentities.DeleteForUserAndRevokeSessions(ctx, f.user.ID, identities[0].ID, f.user.AuthSessionVersion, true)
				if err == nil && !deleted {
					err = fmt.Errorf("unlink deleted nothing")
				}
				return err
			},
		},
	}

	for dialect, open := range dialects {
		t.Run(dialect, func(t *testing.T) {
			ctx := context.Background()
			database := open(t)
			repositories := NewRepositories(database)
			file := security.NewCalendarFeedFenceFile(filepath.Join(t.TempDir(), "calendar-feed.fence"))
			fence := services.NewCalendarFeedRestoreFence(repositories.AppState, repositories.Users, file)
			repositories = repositories.WithCalendarFeedFence(fence)
			if outcome, err := fence.EnforceRequired(ctx); err != nil || !outcome.FirstBoot {
				t.Fatalf("first boot: %+v, %v", outcome, err)
			}
			auth := services.NewAuthService(repositories.Users)
			now := time.Now()

			// Sequential on one database: each boot below leaves the halves
			// agreeing again, which is the state the next case starts from.
			for _, revocation := range revocations {
				t.Run(revocation.name, func(t *testing.T) {
					user := createUserForTimezoneTest(t, repositories.Users, revocation.name+"@example.com")
					f := fixture{repositories: repositories, user: user}
					if revocation.setup != nil {
						revocation.setup(t, f)
					}
					backup := takeRestoreSnapshot(t, database, user.ID)
					f.user = backup.user
					cookie, _, err := auth.BuildAuthSessionTokenWithSessionID(restoreAcrossRevocationSecret, user.ID, backup.user.Role, backup.user.AuthSessionVersion, time.Hour, now)
					if err != nil {
						t.Fatalf("mint the cookie the backup's session version accepts: %v", err)
					}

					if err := revocation.revoke(ctx, f); err != nil {
						t.Fatalf("revoke: %v", err)
					}
					restoreRestoreSnapshot(t, database, backup)
					if _, _, err := auth.ResolveAuthSession(ctx, restoreAcrossRevocationSecret, cookie, now); err != nil {
						t.Fatalf("fixture: before the boot, the restored row should accept the revoked cookie again, got %v", err)
					}

					outcome, err := fence.EnforceRequired(ctx)
					if err != nil {
						t.Fatalf("Enforce: %v", err)
					}
					if !outcome.ContinuityBroken || outcome.Revoked.LocalSignIns == 0 {
						t.Fatalf("the boot over a restore across a %s must read the restore and revoke, got %+v", revocation.name, outcome)
					}
					if _, _, err := auth.ResolveAuthSession(ctx, restoreAcrossRevocationSecret, cookie, now); !errors.Is(err, services.ErrAuthSessionTokenRevoked) {
						t.Fatalf("the cookie the %s revoked must not resolve after the boot, got %v", revocation.name, err)
					}
					after := reloadUserForWebhookRevocationTest(t, repositories.Users, user.ID)
					if after.PasswordHash != "" || after.LocalAuthEnabled {
						t.Fatal("the restored password must not sign in after the boot")
					}
				})
			}
		})
	}
}
