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

// sessionVersionCase is one account state a revoking write meets: the version
// stored on the row, the version the caller verified, and what the write must
// leave behind.
type sessionVersionCase struct {
	name        string
	stored      int
	expected    int
	wantErr     error
	wantVersion int
}

// sessionVersionRevokedInBetween names the case the compare-and-set exists
// for: a revocation committed after the caller verified its version.
const sessionVersionRevokedInBetween = "revoked since it was verified"

func sessionVersionCases() []sessionVersionCase {
	return []sessionVersionCase{
		{name: "verified version", stored: 3, expected: 3, wantVersion: 4},
		{name: sessionVersionRevokedInBetween, stored: 4, expected: 3, wantErr: models.ErrAuthSessionVersionChanged, wantVersion: 4},
		{name: "legacy zero row read as version 1", stored: 0, expected: 1, wantVersion: 2},
		{name: "legacy zero expected on a legacy row", stored: 0, expected: 0, wantVersion: 2},
		{name: "legacy zero row revoked since it was read", stored: 0, expected: 2, wantErr: models.ErrAuthSessionVersionChanged, wantVersion: 0},
	}
}

// sessionVersionFixture seeds accounts at a chosen session version and reads
// back what a write left on them.
type sessionVersionFixture struct {
	t        *testing.T
	database *gorm.DB
	seeded   int
}

func (fixture *sessionVersionFixture) seed(version int) uint {
	fixture.t.Helper()
	fixture.seeded++
	email := fmt.Sprintf("session-cas-%d@example.com", fixture.seeded)
	if err := fixture.database.Exec(
		`INSERT INTO users (email, password_hash, recovery_code_hash, totp_secret, cycle_length, role, created_at, local_auth_enabled, auth_session_version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "seeded-hash", "seeded-recovery", "seeded-ciphertext", 35, "owner", time.Now().UTC(), true, version,
	).Error; err != nil {
		fixture.t.Fatalf("insert %s: %v", email, err)
	}
	var userID uint
	if err := fixture.database.Raw(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID).Error; err != nil || userID == 0 {
		fixture.t.Fatalf("resolve %s: %v (id %d)", email, err, userID)
	}
	return userID
}

func (fixture *sessionVersionFixture) read(userID uint, column string) (int, string) {
	fixture.t.Helper()
	var row struct {
		Version int
		Value   string
	}
	if err := fixture.database.Raw(`SELECT auth_session_version AS version, CAST(`+column+` AS TEXT) AS value FROM users WHERE id = ?`, userID).Scan(&row).Error; err != nil {
		fixture.t.Fatalf("load %d: %v", userID, err)
	}
	return row.Version, row.Value
}

// mismatch runs write against a freshly seeded account in the state tc
// describes and reports how the outcome departs from tc, or "" when the write
// did exactly what tc requires.
func (fixture *sessionVersionFixture) mismatch(repo *UserRepository, write sessionVersionWriteUnderTest, tc sessionVersionCase) string {
	fixture.t.Helper()
	userID := fixture.seed(tc.stored)
	_, before := fixture.read(userID, write.column)
	err := write.write(repo, userID, tc.expected)
	if !errors.Is(err, tc.wantErr) {
		return fmt.Sprintf("expected error %v, got %v", tc.wantErr, err)
	}
	version, after := fixture.read(userID, write.column)
	if version != tc.wantVersion {
		return fmt.Sprintf("expected stored version %d, got %d", tc.wantVersion, version)
	}
	if written := after != before; written != (tc.wantErr == nil) {
		return fmt.Sprintf("expected written=%v, %s went %q -> %q", tc.wantErr == nil, write.column, before, after)
	}
	return ""
}

func assertRevokingWritesMoveOnlyFromTheVerifiedSessionVersion(t *testing.T, database *gorm.DB) {
	t.Helper()
	repo := NewUserRepository(database)
	fixture := &sessionVersionFixture{t: t, database: database}

	for _, write := range sessionVersionWritesUnderTest() {
		for _, tc := range sessionVersionCases() {
			if mismatch := fixture.mismatch(repo, write, tc); mismatch != "" {
				t.Fatalf("%s, %s: %s", write.name, tc.name, mismatch)
			}
		}

		if err := write.write(repo, 99999, 1); !errors.Is(err, ErrUserOwnerRequired) {
			t.Fatalf("%s: expected ErrUserOwnerRequired for a missing account, got %v", write.name, err)
		}

		// Concurrent writes verified against the same version: the first
		// commit moves the version, so every other one must find it moved and
		// roll back.
		racer := fixture.seed(1)
		const contenders = 3
		for round := range 5 {
			from, _ := fixture.read(racer, write.column)
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
			if got, _ := fixture.read(racer, write.column); got != from+1 {
				t.Fatalf("%s round %d: expected version %d, got %d", write.name, round, from+1, got)
			}
		}
	}
}

// Negative control for the cases above: they must tell the compare-and-set
// apart from the write shape it replaced, where every revoking write changed
// its column and added one to whatever version it found. Run through the same
// fixture and cases, that shape passes the verified-version case and must
// fail the revoked-in-between one, where it lands one past the revocation —
// the version its caller then mints a session at. A case table or a mismatch
// check that stopped comparing would let the old shape through, and the test
// above would stay green while checking nothing.
func TestSessionVersionCasesRejectTheUnconditionalIncrement(t *testing.T) {
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "user-session-cas-control.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repo := NewUserRepository(database)
	fixture := &sessionVersionFixture{t: t, database: database}
	increment := sessionVersionWriteUnderTest{name: "unconditional increment", column: "password_hash", write: func(_ *UserRepository, userID uint, _ int) error {
		return database.Model(&models.User{}).Where("id = ?", userID).UpdateColumns(map[string]any{
			"password_hash":        "changed-hash",
			"auth_session_version": gorm.Expr("auth_session_version + 1"),
		}).Error
	}}

	verdicts := make(map[string]string)
	for _, tc := range sessionVersionCases() {
		verdicts[tc.name] = fixture.mismatch(repo, increment, tc)
	}
	if mismatch, present := verdicts["verified version"]; !present || mismatch != "" {
		t.Fatalf("expected the old shape to pass the verified-version case (present=%v), got %q", present, mismatch)
	}
	if mismatch, present := verdicts[sessionVersionRevokedInBetween]; !present || mismatch == "" {
		t.Fatalf("expected the %q case to reject the unconditional increment (present=%v)", sessionVersionRevokedInBetween, present)
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
