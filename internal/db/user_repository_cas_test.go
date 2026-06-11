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
	database, err := OpenSQLite(filepath.Join(dir, "cas_test.db"))
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
		context.Background(), user.ID, "old-hash", "new-hash", "new-recovery",
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
		context.Background(), user.ID, "old-hash", "another-hash", "another-recovery",
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
