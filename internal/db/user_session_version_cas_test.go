package db

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// storedSessionVersionForTest reads the account's auth_session_version as the
// caller of a revoking write would have verified it.
func storedSessionVersionForTest(t *testing.T, repo *UserRepository, userID uint) int {
	t.Helper()
	user, err := repo.FindByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("load session version of %d: %v", userID, err)
	}
	return user.AuthSessionVersion
}

// sessionVersionWriteUnderTest is one revoking write that precedes a session
// mint. column is a column the write changes, so a refused write can be shown
// to have written nothing.
type sessionVersionWriteUnderTest struct {
	name   string
	column string
	write  func(repo *UserRepository, userID uint, expected int) error
}

func sessionVersionWritesUnderTest() []sessionVersionWriteUnderTest {
	ctx := context.Background()
	return []sessionVersionWriteUnderTest{
		{name: "UpdatePasswordAndRevokeSessions", column: "password_hash", write: func(repo *UserRepository, userID uint, expected int) error {
			return repo.UpdatePasswordAndRevokeSessions(ctx, userID, expected, "changed-hash", false)
		}},
		{name: "UpdateRecoveryCodeHashAndRevokeSessions", column: "recovery_code_hash", write: func(repo *UserRepository, userID uint, expected int) error {
			return repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, userID, expected, "rotated-recovery", nil)
		}},
		{name: "UpdatePasswordRecoveryCodeAndRevokeSessions", column: "password_hash", write: func(repo *UserRepository, userID uint, expected int) error {
			return repo.UpdatePasswordRecoveryCodeAndRevokeSessions(ctx, userID, expected, "enrolled-hash", "enrolled-recovery", false, nil)
		}},
		{name: "UpdateTOTPFieldsAndRevokeSessions", column: "totp_secret", write: func(repo *UserRepository, userID uint, expected int) error {
			return repo.UpdateTOTPFieldsAndRevokeSessions(ctx, userID, expected, "reenrolled-ciphertext", true)
		}},
		{name: "ClearAllDataAndResetSettings", column: "cycle_length", write: func(repo *UserRepository, userID uint, expected int) error {
			return repo.ClearAllDataAndResetSettings(ctx, userID, expected)
		}},
	}
}

// Every revoking write that precedes a session mint revokes only from the
// version its caller verified factors against. A revocation committed in
// between (a sign-out everywhere, a password change on another device) must
// roll the write back, or the caller mints a session at the version that
// revocation produced and outlives it. Both engines run the same cases: the
// guarantee rests on the UPDATE's predicate being re-evaluated after a lock
// wait, which the two engines provide differently.
func TestUserRepositoryRevokingWritesMoveOnlyFromTheVerifiedSessionVersion(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "user-session-cas.db")})
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		t.Cleanup(func() {
			if sqlDB, err := database.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		assertRevokingWritesMoveOnlyFromTheVerifiedSessionVersion(t, database)
	})

	t.Run("postgres", func(t *testing.T) {
		assertRevokingWritesMoveOnlyFromTheVerifiedSessionVersion(t, openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t)))
	})
}

func assertRevokingWritesMoveOnlyFromTheVerifiedSessionVersion(t *testing.T, database *gorm.DB) {
	t.Helper()
	repo := NewUserRepository(database)

	seeded := 0
	seed := func(version int) uint {
		t.Helper()
		seeded++
		email := fmt.Sprintf("session-cas-%d@example.com", seeded)
		if err := database.Exec(
			`INSERT INTO users (email, password_hash, recovery_code_hash, totp_secret, cycle_length, role, created_at, local_auth_enabled, auth_session_version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			email, "seeded-hash", "seeded-recovery", "seeded-ciphertext", 35, "owner", time.Now().UTC(), true, version,
		).Error; err != nil {
			t.Fatalf("insert %s: %v", email, err)
		}
		var userID uint
		if err := database.Raw(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID).Error; err != nil || userID == 0 {
			t.Fatalf("resolve %s: %v (id %d)", email, err, userID)
		}
		return userID
	}
	read := func(userID uint, column string) (int, string) {
		t.Helper()
		var row struct {
			Version int
			Value   string
		}
		if err := database.Raw(`SELECT auth_session_version AS version, CAST(`+column+` AS TEXT) AS value FROM users WHERE id = ?`, userID).Scan(&row).Error; err != nil {
			t.Fatalf("load %d: %v", userID, err)
		}
		return row.Version, row.Value
	}

	cases := []struct {
		name        string
		stored      int
		expected    int
		wantErr     error
		wantVersion int
	}{
		{name: "verified version", stored: 3, expected: 3, wantVersion: 4},
		{name: "revoked since it was verified", stored: 4, expected: 3, wantErr: models.ErrAuthSessionVersionChanged, wantVersion: 4},
		{name: "legacy zero row read as version 1", stored: 0, expected: 1, wantVersion: 2},
		{name: "legacy zero expected on a legacy row", stored: 0, expected: 0, wantVersion: 2},
		{name: "legacy zero row revoked since it was read", stored: 0, expected: 2, wantErr: models.ErrAuthSessionVersionChanged, wantVersion: 0},
	}
	for _, write := range sessionVersionWritesUnderTest() {
		for _, tc := range cases {
			userID := seed(tc.stored)
			_, before := read(userID, write.column)
			err := write.write(repo, userID, tc.expected)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s, %s: expected error %v, got %v", write.name, tc.name, tc.wantErr, err)
			}
			version, after := read(userID, write.column)
			if version != tc.wantVersion {
				t.Fatalf("%s, %s: expected stored version %d, got %d", write.name, tc.name, tc.wantVersion, version)
			}
			if written := after != before; written != (tc.wantErr == nil) {
				t.Fatalf("%s, %s: expected written=%v, %s went %q -> %q", write.name, tc.name, tc.wantErr == nil, write.column, before, after)
			}
		}

		if err := write.write(repo, 99999, 1); !errors.Is(err, ErrUserOwnerRequired) {
			t.Fatalf("%s: expected ErrUserOwnerRequired for a missing account, got %v", write.name, err)
		}

		// Concurrent writes verified against the same version: the first
		// commit moves the version, so every other one must find it moved and
		// roll back.
		racer := seed(1)
		const contenders = 3
		for round := range 5 {
			from, _ := read(racer, write.column)
			var wg sync.WaitGroup
			start := make(chan struct{})
			results := make([]error, contenders)
			for index := range contenders {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					<-start
					results[index] = write.write(repo, racer, from)
				}(index)
			}
			close(start)
			wg.Wait()

			wins := 0
			for index, err := range results {
				switch {
				case err == nil:
					wins++
				case errors.Is(err, models.ErrAuthSessionVersionChanged):
				default:
					t.Fatalf("%s round %d contender %d: unexpected error %v", write.name, round, index, err)
				}
			}
			if wins != 1 {
				t.Fatalf("%s round %d: expected exactly one write to win, got %d (%v)", write.name, round, wins, results)
			}
			if got, _ := read(racer, write.column); got != from+1 {
				t.Fatalf("%s round %d: expected version %d, got %d", write.name, round, from+1, got)
			}
		}
	}
}

// Negative control for the case above: the increment every revoking write
// used before the compare-and-set, run against the same account revoked since
// its caller verified version 3, succeeds and lands on a version one past the
// revocation — the version the caller would then mint its session at, which
// no longer reflects that the revocation happened. The compare-and-set against
// the same row refuses.
func TestUnconditionalSessionVersionIncrementCarriesAnInterveningRevocation(t *testing.T) {
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "user-session-cas-control.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at, local_auth_enabled, auth_session_version) VALUES (?, ?, ?, ?, ?, ?)`,
		"session-cas-control@example.com", "hash", "owner", time.Now().UTC(), true, 4,
	).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	var userID uint
	if err := database.Raw(`SELECT id FROM users WHERE email = ?`, "session-cas-control@example.com").Scan(&userID).Error; err != nil || userID == 0 {
		t.Fatalf("resolve: %v (id %d)", err, userID)
	}
	const verified = 3
	if err := database.Transaction(func(tx *gorm.DB) error {
		_, err := updateFromAuthSessionVersionTx(tx, userID, verified, nil)
		return err
	}); !errors.Is(err, models.ErrAuthSessionVersionChanged) {
		t.Fatalf("expected the compare-and-set to refuse, got %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", userID).UpdateColumn("auth_session_version", gorm.Expr("auth_session_version + 1")).Error; err != nil {
		t.Fatalf("unconditional increment: %v", err)
	}
	if got := storedSessionVersionForTest(t, NewUserRepository(database), userID); got != verified+2 {
		t.Fatalf("expected the unconditional increment to carry the revocation to %d, got %d", verified+2, got)
	}
}

// The shared compare-and-set refuses a zero owner before it builds a query,
// like every other users-table writer.
func TestUpdateFromAuthSessionVersionRefusesAZeroOwner(t *testing.T) {
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "user-session-cas-zero.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if _, err := updateFromAuthSessionVersionTx(database, 0, 1, nil); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("expected ErrUserOwnerRequired for a zero owner, got %v", err)
	}
}
