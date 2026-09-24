package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestUpdatePasswordRecoveryCodeAndRevokeSessionsCASRejectsReplay asserts that
// the CAS UPDATE is atomically single-use at the DB layer:
//   - First call with the correct oldPasswordHash succeeds (RowsAffected == 1)
//     and bumps auth_session_version exactly once.
//   - Second call with the same oldPasswordHash returns
//     ErrResetTokenAlreadyConsumed because the stored hash already changed.
func TestUpdatePasswordRecoveryCodeAndRevokeSessionsCASRejectsReplay(t *testing.T) {
	dir := t.TempDir()
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(dir, "cas_test.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	repo := NewUserRepository(database)

	user := &models.User{
		Email:              "cas@example.com",
		PasswordHash:       "old-hash",
		RecoveryCodeHash:   "old-recovery",
		LocalAuthEnabled:   true,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
		CycleLength:        models.DefaultCycleLength,
		PeriodLength:       models.DefaultPeriodLength,
		AutoPeriodFill:     true,
		CreatedAt:          time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// First CAS call — must succeed and bump session version.
	err = repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(
		context.Background(), user.ID, "old-hash", 1, "new-hash", "new-recovery", nil,
	)
	if err != nil {
		t.Fatalf("first CAS call: unexpected error: %v", err)
	}

	got, err := repo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("find after first CAS: %v", err)
	}
	if got.PasswordHash != "new-hash" {
		t.Fatalf("expected password_hash 'new-hash', got %q", got.PasswordHash)
	}
	if got.RecoveryCodeHash != "new-recovery" {
		t.Fatalf("expected recovery_code_hash 'new-recovery', got %q", got.RecoveryCodeHash)
	}
	if got.AuthSessionVersion != 2 {
		t.Fatalf("expected auth_session_version 2 after first CAS, got %d", got.AuthSessionVersion)
	}

	// Second CAS call with the SAME oldPasswordHash — must be rejected.
	err = repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(
		context.Background(), user.ID, "old-hash", 1, "another-hash", "another-recovery", nil,
	)
	if !errors.Is(err, ErrResetTokenAlreadyConsumed) {
		t.Fatalf("second CAS call: expected ErrResetTokenAlreadyConsumed, got %v", err)
	}

	// auth_session_version must still be 2 (only one write won).
	got2, err := repo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("find after second CAS: %v", err)
	}
	if got2.AuthSessionVersion != 2 {
		t.Fatalf("expected auth_session_version to remain 2, got %d", got2.AuthSessionVersion)
	}
	if got2.PasswordHash != "new-hash" {
		t.Fatalf("expected password_hash to remain 'new-hash', got %q", got2.PasswordHash)
	}
}

// TestUpdatePasswordRecoveryCodeAndRevokeSessionsCASLosesToASessionVersionBump
// pins the auth_session_version term of the reset CAS: the reset token is
// checked against the version when it is resolved, and a revocation or posture
// change that bumps the column between that read and the UPDATE must make the
// reset lose, even though the password hash is unchanged. A legacy row still
// holding 0 is read as version 1 and must keep matching that value.
func TestUpdatePasswordRecoveryCodeAndRevokeSessionsCASLosesToASessionVersionBump(t *testing.T) {
	dir := t.TempDir()
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(dir, "cas_version_test.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repo := NewUserRepository(database)
	ctx := context.Background()

	user := &models.User{
		Email:              "cas-version@example.com",
		PasswordHash:       "old-hash",
		RecoveryCodeHash:   "old-recovery",
		LocalAuthEnabled:   true,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
		CycleLength:        models.DefaultCycleLength,
		PeriodLength:       models.DefaultPeriodLength,
		AutoPeriodFill:     true,
		CreatedAt:          time.Now().UTC(),
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// The reset read the row at version 1; a revocation lands before the write.
	if err := repo.BumpAuthSessionVersion(ctx, user.ID); err != nil {
		t.Fatalf("bump session version: %v", err)
	}
	err = repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, user.ID, "old-hash", 1, "new-hash", "new-recovery", nil)
	if !errors.Is(err, ErrResetTokenAlreadyConsumed) {
		t.Fatalf("expected a reset read before the version bump to lose, got %v", err)
	}
	got, err := repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("find after lost CAS: %v", err)
	}
	if got.PasswordHash != "old-hash" || got.AuthSessionVersion != 2 {
		t.Fatalf("a lost reset must write nothing, got hash %q version %d", got.PasswordHash, got.AuthSessionVersion)
	}

	// A version the caller never read is refused outright.
	if err := repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, user.ID, "old-hash", 0, "new-hash", "new-recovery", nil); !errors.Is(err, ErrResetTokenAlreadyConsumed) {
		t.Fatalf("expected a zero session version to be refused, got %v", err)
	}

	// A legacy row holding 0 is version 1 to the application.
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).UpdateColumn("auth_session_version", 0).Error; err != nil {
		t.Fatalf("seed legacy version: %v", err)
	}
	if err := repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, user.ID, "old-hash", 2, "new-hash", "new-recovery", nil); !errors.Is(err, ErrResetTokenAlreadyConsumed) {
		t.Fatalf("expected a legacy 0 row not to match version 2, got %v", err)
	}
	if err := repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, user.ID, "old-hash", 1, "new-hash", "new-recovery", nil); err != nil {
		t.Fatalf("expected a legacy 0 row to match version 1, got %v", err)
	}
	got, err = repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("find after legacy CAS: %v", err)
	}
	if got.PasswordHash != "new-hash" || got.AuthSessionVersion != 1 {
		t.Fatalf("expected the legacy reset to land (hash new-hash, version 0+1), got hash %q version %d", got.PasswordHash, got.AuthSessionVersion)
	}
}

// TestOpenSQLiteConnectionPoolLimits verifies that the connection pool is
// configured with concrete non-zero bounds after openSQLiteConnection. The
// values must match the documented limits (max open == max idle == 4).
func TestOpenSQLiteConnectionPoolLimits(t *testing.T) {
	dir := t.TempDir()
	database, err := openSQLiteConnection(filepath.Join(dir, "pool_test.db"))
	if err != nil {
		t.Fatalf("openSQLiteConnection: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("database.DB(): %v", err)
	}
	stats := sqlDB.Stats()

	// MaxOpenConnections == 0 means unbounded (the pre-fix state). Assert it
	// is now set to exactly 4.
	if stats.MaxOpenConnections != 4 {
		t.Fatalf("expected MaxOpenConnections 4, got %d", stats.MaxOpenConnections)
	}
}

// TestUpgradePasswordHashCAS runs every case on both shipped drivers, one
// database per driver: the predicate's row count is the driver's to report, so
// SQLite alone would not pin what a Postgres deployment does. Each case seeds
// its own row.
func TestUpgradePasswordHashCAS(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, repo *UserRepository)
	}{
		{"preserves the session version", testUpgradePasswordHashCASPreservesSessionVersion},
		{"loses to a credential write", testUpgradePasswordHashCASLosesToACredentialWrite},
	}
	configs := map[string]func(t *testing.T) Config{
		"sqlite": func(t *testing.T) Config {
			return Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "rehash_test.db")}
		},
		"postgres": startPostgresTestConfig,
	}
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			database, err := OpenDatabase(configs[driver](t))
			if err != nil {
				t.Fatalf("open %s: %v", driver, err)
			}
			t.Cleanup(func() {
				if sqlDB, err := database.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})
			repo := NewUserRepository(database)
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) { tc.run(t, repo) })
			}
		})
	}
}

func createUpgradePasswordHashCASUser(t *testing.T, repo *UserRepository, email string) *models.User {
	t.Helper()

	user := &models.User{
		Email:              email,
		PasswordHash:       "legacy-hash",
		RecoveryCodeHash:   "recovery-hash",
		LocalAuthEnabled:   true,
		MustChangePassword: false,
		AuthSessionVersion: 4,
		Role:               models.RoleOwner,
		CycleLength:        models.DefaultCycleLength,
		PeriodLength:       models.DefaultPeriodLength,
		AutoPeriodFill:     true,
		CreatedAt:          time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

// testUpgradePasswordHashCASPreservesSessionVersion asserts the transparent
// bcrypt-cost upgrade rewrites password_hash without bumping
// auth_session_version and without disturbing the other credential columns —
// the account's security posture is unchanged, so no active session may be
// revoked by an internal storage upgrade.
func testUpgradePasswordHashCASPreservesSessionVersion(t *testing.T, repo *UserRepository) {
	user := createUpgradePasswordHashCASUser(t, repo, "rehash-applied@example.com")

	applied, err := repo.UpgradePasswordHashCAS(context.Background(), user.ID, "legacy-hash", "upgraded-hash")
	if err != nil {
		t.Fatalf("UpgradePasswordHashCAS: %v", err)
	}
	if !applied {
		t.Fatal("the upgrade against the current hash reported not applied")
	}

	got, err := repo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("find after rehash: %v", err)
	}
	if got.PasswordHash != "upgraded-hash" {
		t.Fatalf("expected password_hash 'upgraded-hash', got %q", got.PasswordHash)
	}
	if got.AuthSessionVersion != 4 {
		t.Fatalf("expected auth_session_version to stay 4 (no revoke), got %d", got.AuthSessionVersion)
	}
	if got.RecoveryCodeHash != "recovery-hash" {
		t.Fatalf("expected recovery_code_hash untouched, got %q", got.RecoveryCodeHash)
	}
	if !got.LocalAuthEnabled {
		t.Fatal("expected local_auth_enabled untouched (true)")
	}
}

// testUpgradePasswordHashCASLosesToACredentialWrite is the predicate itself: a
// password change that lands after the login read the hash must survive the
// upgrade that login then attempts. Without the predicate the upgrade would
// write the old password back, with no session-version bump.
func testUpgradePasswordHashCASLosesToACredentialWrite(t *testing.T, repo *UserRepository) {
	user := createUpgradePasswordHashCASUser(t, repo, "rehash-lost@example.com")
	if err := repo.UpdatePasswordAndRevokeSessions(context.Background(), user.ID, "changed-hash", false); err != nil {
		t.Fatalf("UpdatePasswordAndRevokeSessions: %v", err)
	}

	applied, err := repo.UpgradePasswordHashCAS(context.Background(), user.ID, "legacy-hash", "upgraded-legacy-hash")
	if err != nil {
		t.Fatalf("a lost race is not a database failure, got %v", err)
	}
	if applied {
		t.Fatal("the upgrade reported applied against a hash the row no longer holds")
	}

	got, err := repo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("find after lost upgrade: %v", err)
	}
	if got.PasswordHash != "changed-hash" {
		t.Fatalf("password_hash = %q after the lost upgrade, want the changed credential kept", got.PasswordHash)
	}
	if got.AuthSessionVersion != 5 {
		t.Fatalf("auth_session_version = %d, want 5: bumped once by the change, never by the upgrade", got.AuthSessionVersion)
	}
}

// TestUpgradePasswordHashCASFailsClosedWhenTheUpdateErrors covers the error
// arm: an upgrade that could not run must report the failure AND read as not
// applied, or the caller would adopt a hash the row never received.
func TestUpgradePasswordHashCASFailsClosedWhenTheUpdateErrors(t *testing.T) {
	repo := openRevealMarkRepoForTest(t)
	user := createUpgradePasswordHashCASUser(t, repo, "rehash-closed@example.com")
	closeRevealMarkRepoHandle(t, repo)

	applied, err := repo.UpgradePasswordHashCAS(context.Background(), user.ID, "legacy-hash", "upgraded-hash")
	if err == nil {
		t.Fatal("expected UpgradePasswordHashCAS against a closed database to surface an error")
	}
	if applied {
		t.Fatal("an upgrade that errored must never read as applied")
	}
}
