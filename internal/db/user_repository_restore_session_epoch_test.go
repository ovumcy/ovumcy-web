package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

var restoreSessionEpochSecret = []byte("restore-session-epoch-key-000001")

// authServiceUnderStoredEpoch is the service a boot builds: the fence pass has
// already run, so the epoch it loads from app_state is the one in force.
func authServiceUnderStoredEpoch(t *testing.T, repositories *Repositories) *services.AuthService {
	t.Helper()
	epoch, _, err := services.LoadAuthSessionEpoch(context.Background(), repositories.AppState)
	if err != nil {
		t.Fatalf("load session epoch: %v", err)
	}
	auth := services.NewAuthService(repositories.Users)
	auth.ConfigureSessionEpoch(epoch)
	return auth
}

// bootedAuthServiceAfterRestore is the service the boot over a restore builds.
// It asserts the precondition every refusal below rests on: the epoch was
// already stored (not freshly created by this load) and the fence pass rotated
// it away from the one the pre-restore service minted under. Without that, a
// refusal could come from a brand-new epoch and say nothing about the rotation.
func bootedAuthServiceAfterRestore(t *testing.T, repositories *Repositories, preRestore *services.AuthService) *services.AuthService {
	t.Helper()
	before, err := preRestore.SessionEpoch()
	if err != nil {
		t.Fatalf("pre-restore epoch: %v", err)
	}
	epoch, created, err := services.LoadAuthSessionEpoch(context.Background(), repositories.AppState)
	if err != nil {
		t.Fatalf("load session epoch: %v", err)
	}
	if created {
		t.Fatal("the restore boot must find the rotated epoch stored, not create one")
	}
	if epoch == before {
		t.Fatal("the restore boot must rotate the session epoch away from the pre-restore one")
	}
	booted := services.NewAuthService(repositories.Users)
	booted.ConfigureSessionEpoch(epoch)
	return booted
}

// restoreCounterTo puts the users id counter back to maxID the way a restore
// of a dump does: sqlite_sequence under AUTOINCREMENT, setval on Postgres.
func restoreUsersCounterTo(t *testing.T, database *gorm.DB, maxID uint) {
	t.Helper()
	var err error
	switch database.Name() {
	case "sqlite":
		err = database.Exec("UPDATE sqlite_sequence SET seq = ? WHERE name = 'users'", maxID).Error
	default:
		err = database.Exec("SELECT setval(pg_get_serial_sequence('users', 'id'), ?)", maxID).Error
	}
	if err != nil {
		t.Fatalf("restore the users id counter: %v", err)
	}
}

func openRestoreSessionEpochFixture(t *testing.T, dialect string) (*gorm.DB, *Repositories, *services.CalendarFeedRestoreFence) {
	t.Helper()
	var database *gorm.DB
	if dialect == "sqlite" {
		database = openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "restore-session-epoch.db"))
	} else {
		database = openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
	}
	repositories := NewRepositories(database)
	file := security.NewCalendarFeedFenceFile(filepath.Join(t.TempDir(), "calendar-feed.fence"))
	fence := services.NewCalendarFeedRestoreFence(repositories.AppState, repositories.Users, file)
	repositories = repositories.WithCalendarFeedFence(fence)
	if outcome, err := fence.EnforceRequired(context.Background()); err != nil || !outcome.FirstBoot {
		t.Fatalf("first boot: %+v, %v", outcome, err)
	}
	return database, repositories, fence
}

// TestARestoreRefusesASessionMintedAfterTheBackupAcrossTwoVersionBumps is WEB-538's
// revival regression: the backup holds version V, a password change moves the
// account to V+1 and re-issues the device at V+1, a sign-out moves it to V+2,
// and the restore plus its fence boot bumps the row by exactly one — back to
// V+1, where the revoked cookie matches again. Only the session epoch the boot
// rotates refuses it.
func TestARestoreRefusesASessionMintedAfterTheBackupAcrossTwoVersionBumps(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			ctx := context.Background()
			database, repositories, fence := openRestoreSessionEpochFixture(t, dialect)
			now := time.Now()
			auth := authServiceUnderStoredEpoch(t, repositories)

			user := createUserForTimezoneTest(t, repositories.Users, "two-bumps@example.com")
			backup := takeRestoreSnapshot(t, database, user.ID)
			backupVersion := backup.user.AuthSessionVersion

			if err := repositories.Users.UpdatePasswordAndRevokeSessions(ctx, user.ID, backupVersion, "replacement-hash", false); err != nil {
				t.Fatalf("password change: %v", err)
			}
			afterPassword := reloadUserForWebhookRevocationTest(t, repositories.Users, user.ID)
			if afterPassword.AuthSessionVersion != backupVersion+1 {
				t.Fatalf("the password change must move the version to V+1, got %d from %d", afterPassword.AuthSessionVersion, backupVersion)
			}
			cookie, _, err := auth.BuildAuthSessionTokenWithSessionID(restoreSessionEpochSecret, user.ID, afterPassword.Role, afterPassword.AuthSessionVersion, time.Hour, now)
			if err != nil {
				t.Fatalf("mint the V+1 cookie: %v", err)
			}
			if _, _, err := auth.ResolveAuthSession(ctx, restoreSessionEpochSecret, cookie, now); err != nil {
				t.Fatalf("anti-vacuity: the V+1 cookie must be accepted before the sign-out, got %v", err)
			}
			if err := repositories.Users.BumpAuthSessionVersion(ctx, user.ID); err != nil {
				t.Fatalf("sign-out: %v", err)
			}
			if _, _, err := auth.ResolveAuthSession(ctx, restoreSessionEpochSecret, cookie, now); !errors.Is(err, services.ErrAuthSessionTokenRevoked) {
				t.Fatalf("anti-vacuity: the sign-out must revoke the V+1 cookie, got %v", err)
			}

			restoreRestoreSnapshot(t, database, backup)
			outcome, err := fence.EnforceRequired(ctx)
			if err != nil || !outcome.ContinuityBroken {
				t.Fatalf("the boot over the restore must read it, got %+v, %v", outcome, err)
			}
			restored := reloadUserForWebhookRevocationTest(t, repositories.Users, user.ID)
			if restored.AuthSessionVersion != backupVersion+1 {
				t.Fatalf("fixture: the boot bumps the restored row by one, to V+1, got %d from %d", restored.AuthSessionVersion, backupVersion)
			}
			if _, _, err := auth.ResolveAuthSession(ctx, restoreSessionEpochSecret, cookie, now); err != nil {
				t.Fatalf("negative control: under the pre-restore epoch the version alone revives the revoked cookie, got %v", err)
			}

			booted := bootedAuthServiceAfterRestore(t, repositories, auth)
			if _, _, err := booted.ResolveAuthSession(ctx, restoreSessionEpochSecret, cookie, now); !errors.Is(err, services.ErrAuthSessionTokenRevoked) {
				t.Fatalf("the cookie minted at V+1 must not resolve after the restore boot, got %v", err)
			}
		})
	}
}

// TestARestoreRefusesACookieFromAnAccountThatSharesItsIdWithANewSignup is
// WEB-538's id-reuse regression: account N+1 signs up after the backup and
// receives a cookie, the restore rolls the id counter back, and a new signup
// takes N+1 at the default version. Before the epoch, the old cookie resolved
// as that new owner.
func TestARestoreRefusesACookieFromAnAccountThatSharesItsIdWithANewSignup(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			ctx := context.Background()
			database, repositories, fence := openRestoreSessionEpochFixture(t, dialect)
			now := time.Now()
			auth := authServiceUnderStoredEpoch(t, repositories)

			existing := createUserForTimezoneTest(t, repositories.Users, "backed-up@example.com")
			backup := takeRestoreSnapshot(t, database, existing.ID)

			late := createUserForTimezoneTest(t, repositories.Users, "signed-up-after-backup@example.com")
			if late.ID != existing.ID+1 {
				t.Fatalf("fixture: the post-backup account must take id N+1, got %d after %d", late.ID, existing.ID)
			}
			cookie, _, err := auth.BuildAuthSessionTokenWithSessionID(restoreSessionEpochSecret, late.ID, late.Role, late.AuthSessionVersion, time.Hour, now)
			if err != nil {
				t.Fatalf("mint the post-backup cookie: %v", err)
			}
			resolved, _, err := auth.ResolveAuthSession(ctx, restoreSessionEpochSecret, cookie, now)
			if err != nil || resolved.Email != late.Email {
				t.Fatalf("anti-vacuity: the cookie must resolve as the account that signed up, got %v, %v", resolved, err)
			}

			// The fence reads a restore only when some revocation advanced it
			// after the backup, so the backed-up owner signs out once here: the
			// restore then puts a database behind the fence file, as it does in
			// any instance that has revoked anything since its last backup.
			if err := repositories.Users.BumpAuthSessionVersion(ctx, existing.ID); err != nil {
				t.Fatalf("sign the backed-up owner out: %v", err)
			}

			restoreRestoreSnapshot(t, database, backup)
			if err := database.Where("id > ?", existing.ID).Delete(&models.User{}).Error; err != nil {
				t.Fatalf("drop the post-backup account: %v", err)
			}
			restoreUsersCounterTo(t, database, existing.ID)
			outcome, err := fence.EnforceRequired(ctx)
			if err != nil || !outcome.ContinuityBroken {
				t.Fatalf("the boot over the restore must read it, got %+v, %v", outcome, err)
			}

			booted := bootedAuthServiceAfterRestore(t, repositories, auth)
			reused := createUserForTimezoneTest(t, repositories.Users, "reuses-the-id@example.com")
			if reused.ID != late.ID {
				t.Fatalf("fixture: the new signup must reuse id %d, got %d", late.ID, reused.ID)
			}
			if reused.AuthSessionVersion != late.AuthSessionVersion {
				t.Fatalf("fixture: the reused row must start at the version the old cookie carries, %d vs %d", reused.AuthSessionVersion, late.AuthSessionVersion)
			}
			if impostor, _, err := auth.ResolveAuthSession(ctx, restoreSessionEpochSecret, cookie, now); err != nil || impostor.Email != reused.Email {
				t.Fatalf("negative control: under the pre-restore epoch the old cookie resolves as the new owner, got %v, %v", impostor, err)
			}
			if resolved, _, err := booted.ResolveAuthSession(ctx, restoreSessionEpochSecret, cookie, now); !errors.Is(err, services.ErrAuthSessionTokenRevoked) || resolved != nil {
				t.Fatalf("the old cookie must not resolve as the new account after the restore boot, got %v, %v", resolved, err)
			}
		})
	}
}

// TestRevokeRestoredAccessRefusesWhenTheEpochCannotBeStored pins that a failed
// epoch write fails the whole revocation: bumping versions without the rotated
// epoch would revive a revoked cookie.
func TestRevokeRestoredAccessRefusesWhenTheEpochCannotBeStored(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "restore-epoch-store-failure.db"))
	repositories := NewRepositories(database)
	user := models.User{Email: "epoch-store-failure@example.com", Role: models.RoleOwner, LocalAuthEnabled: true, PasswordHash: "x", AuthSessionVersion: 3}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := database.Migrator().DropTable(&models.AppState{}); err != nil {
		t.Fatalf("drop app_state: %v", err)
	}

	counts, err := repositories.Users.RevokeRestoredAccess(context.Background(), "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC")
	if err == nil {
		t.Fatal("an epoch that cannot be stored must fail the revocation")
	}
	if counts != (models.RestoreRevocationCounts{}) {
		t.Fatalf("a refused revocation must report no work, got %+v", counts)
	}
	var reloaded models.User
	if err := database.First(&reloaded, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.AuthSessionVersion != 3 {
		t.Fatalf("a refused revocation must not bump the session version, got %d", reloaded.AuthSessionVersion)
	}
}
