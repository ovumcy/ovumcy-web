package db

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigration041BackfillsLegacyAuthSessionVersionToOne locks the data
// contract of migration 041 (WEB-50/WEB-65, F1 of the WEB-12 security audit):
// a row whose auth_session_version is stored at 0 or below is raised to 1, and
// a row already at 1 or above is left exactly where it is. Column 008 shipped
// NOT NULL DEFAULT 1, so a stored value <= 0 only exists on a row someone set
// by hand -- the shape a restored legacy backup or a hand-edited row can still
// carry.
//
// The surviving unaffected rows are the positive anchor -- a migration that
// clamped every row to 1 would satisfy the "no row is <= 0 afterward" half on
// its own.
func TestMigration041BackfillsLegacyAuthSessionVersionToOne(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-041.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	seed := func(email string, version int) uint {
		t.Helper()
		if err := database.Exec(
			`INSERT INTO users (email, password_hash, role, created_at, auth_session_version)
			 VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?)`,
			email, "test-hash", "owner", version,
		).Error; err != nil {
			t.Fatalf("seed %s at version %d: %v", email, version, err)
		}
		var userID uint
		if err := database.Raw(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID).Error; err != nil || userID == 0 {
			t.Fatalf("resolve %s: %v (id %d)", email, err, userID)
		}
		return userID
	}

	zero := seed("legacy-zero-041@example.com", 0)
	negative := seed("legacy-negative-041@example.com", -3)
	current := seed("current-041@example.com", 1)
	ahead := seed("ahead-041@example.com", 7)

	if err := database.Exec(`DELETE FROM schema_migrations WHERE version = ?`, "041").Error; err != nil {
		t.Fatalf("delete the migration 041 record: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	reopened := openSQLiteForMigrationBootstrapTest(t, databasePath)
	assertAllEmbeddedMigrationsApplied(t, reopened)

	readVersion := func(userID uint) int {
		t.Helper()
		var version int
		if err := reopened.Raw(`SELECT auth_session_version FROM users WHERE id = ?`, userID).Scan(&version).Error; err != nil {
			t.Fatalf("read version of %d: %v", userID, err)
		}
		return version
	}

	for _, tc := range []struct {
		name   string
		userID uint
		want   int
	}{
		{"a row stored at 0 is raised to 1", zero, 1},
		{"a row stored below 0 is raised to 1", negative, 1},
		{"a row already at 1 is untouched", current, 1},
		{"a row ahead of 1 is untouched", ahead, 7},
	} {
		if got := readVersion(tc.userID); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestMigration041ABumpOnABackfilledLegacyRowRevokesALiveSession pins the
// invariant the backfill exists to establish: after migration 041 has moved a
// legacy 0 row to 1, the FIRST revoking write against it writes a version no
// grant minted before that write can read as current. Before this migration,
// the same bump wrote 0 + 1 = 1 -- the version every prior grant already
// normalized to -- so the "revocation" left every session valid.
func TestMigration041ABumpOnABackfilledLegacyRowRevokesALiveSession(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-041-bump.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	email := "legacy-bump-041@example.com"
	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at, auth_session_version)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		email, "test-hash", "owner", 0,
	).Error; err != nil {
		t.Fatalf("seed legacy row at version 0: %v", err)
	}
	var userID uint
	if err := database.Raw(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID).Error; err != nil || userID == 0 {
		t.Fatalf("resolve seeded row: %v (id %d)", err, userID)
	}

	if err := database.Exec(`DELETE FROM schema_migrations WHERE version = ?`, "041").Error; err != nil {
		t.Fatalf("delete the migration 041 record: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	reopened := openSQLiteForMigrationBootstrapTest(t, databasePath)
	repo := NewUserRepository(reopened)

	var backfilled int
	if err := reopened.Raw(`SELECT auth_session_version FROM users WHERE id = ?`, userID).Scan(&backfilled).Error; err != nil {
		t.Fatalf("read backfilled version: %v", err)
	}
	if backfilled != 1 {
		t.Fatalf("expected the migration to raise the legacy row to 1, got %d", backfilled)
	}

	// A grant minted against the row before this instance ever ran the
	// backfill would have carried NormalizeAuthSessionVersion(0) == 1 -- the
	// exact value now stored. Simulate a revoking write (sign-out everywhere)
	// and require the result to strictly exceed that grant's version, so the
	// grant no longer matches current.
	ctx := context.Background()
	if err := repo.BumpAuthSessionVersion(ctx, userID); err != nil {
		t.Fatalf("bump auth session version: %v", err)
	}
	var afterBump int
	if err := reopened.Raw(`SELECT auth_session_version FROM users WHERE id = ?`, userID).Scan(&afterBump).Error; err != nil {
		t.Fatalf("read version after bump: %v", err)
	}
	const legacyGrantVersion = 1 // NormalizeAuthSessionVersion(0)
	if afterBump == legacyGrantVersion {
		t.Fatalf("the bump wrote %d, the same version a grant minted against the pre-backfill row at 0 already carries: the revocation is invisible", afterBump)
	}
	if afterBump != 2 {
		t.Fatalf("expected the backfilled row's first bump to land on 2, got %d", afterBump)
	}
}
