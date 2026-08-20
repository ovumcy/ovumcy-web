package services

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// TestResetPasswordAndRotateRecoveryCodeCASScopesTheWriteToTheResolvedOwner
// drives the recovery reset through the REAL user repository with two
// independent owners in one database.
//
// The CAS stub in password_reset_cas_regression_test.go simulates the predicate
// against a single embedded user, so every owner id reaches the same row; the
// db-layer CAS test creates exactly one user, whose id is 1, so it cannot tell
// the resolved owner from a hard-coded 1 either. Here owner A is created first
// and therefore holds id 1, and the reset runs for owner B: A's row — password
// hash, recovery hash and the auth_session_version that carries the session
// invalidation — must be byte-for-byte unchanged.
func TestResetPasswordAndRotateRecoveryCodeCASScopesTheWriteToTheResolvedOwner(t *testing.T) {
	database := newPasswordResetTwoOwnerDatabase(t)
	repository := db.NewUserRepository(database)
	service := NewAuthService(repository)

	ownerA := createPasswordResetTwoOwnerUser(t, database, "reset-owner-a@example.com", "OwnerAPass1")
	ownerB := createPasswordResetTwoOwnerUser(t, database, "reset-owner-b@example.com", "OwnerBPass1")

	// The mutant this test exists to kill is a literal owner `1`; if the first
	// account did not land on id 1 the test would go green while proving
	// nothing.
	if ownerA.ID != 1 {
		t.Fatalf("fixture assumption broken: expected the first owner to hold id 1, got %d", ownerA.ID)
	}
	if ownerB.ID == ownerA.ID {
		t.Fatalf("fixture assumption broken: the two owners must be distinct, both are %d", ownerB.ID)
	}

	before := readPasswordResetTwoOwnerUser(t, database, ownerA.ID)

	target := readPasswordResetTwoOwnerUser(t, database, ownerB.ID)
	oldHash := target.PasswordHash
	recoveryCode, err := service.ResetPasswordAndRotateRecoveryCodeCAS(context.Background(), &target, oldHash, "EvenStronger2")
	if err != nil {
		t.Fatalf("resetting owner B failed with %v — a CAS scoped to a different owner matches no row", err)
	}
	if recoveryCode == "" {
		t.Fatal("expected a rotated recovery code for owner B")
	}

	// Isolation: owner A must not have been touched in any column.
	after := readPasswordResetTwoOwnerUser(t, database, ownerA.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("owner A's row changed while owner B reset their password — the CAS write is not scoped to the resolved owner:\nbefore: %+v\nafter:  %+v", before, after)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("OwnerAPass1")); err != nil {
		t.Fatalf("owner A's password no longer verifies after owner B's reset: %v", err)
	}
	if after.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("owner A's auth_session_version moved from %d to %d — another owner's reset revoked their sessions", before.AuthSessionVersion, after.AuthSessionVersion)
	}

	// Positive anchor: the reset must actually have landed on owner B, or the
	// isolation half above would pass on a service that writes nothing at all.
	gotB := readPasswordResetTwoOwnerUser(t, database, ownerB.ID)
	if err := bcrypt.CompareHashAndPassword([]byte(gotB.PasswordHash), []byte("EvenStronger2")); err != nil {
		t.Fatalf("expected owner B's new password to verify: %v", err)
	}
	if gotB.RecoveryCodeHash == "" || gotB.RecoveryCodeHash == ownerB.RecoveryCodeHash {
		t.Fatal("expected owner B's recovery code hash to be rotated")
	}
	if gotB.AuthSessionVersion != 2 {
		t.Fatalf("expected owner B's auth_session_version to be bumped to 2, got %d", gotB.AuthSessionVersion)
	}
}

func newPasswordResetTwoOwnerDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{
		Driver:     db.DriverSQLite,
		SQLitePath: filepath.Join(t.TempDir(), "ovumcy-reset-two-owner.db"),
	})
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return database
}

func createPasswordResetTwoOwnerUser(t *testing.T, database *gorm.DB, email string, password string) models.User {
	t.Helper()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password for %s: %v", email, err)
	}
	recoveryHash, err := bcrypt.GenerateFromPassword([]byte("recovery-"+email), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash recovery code for %s: %v", email, err)
	}

	user := models.User{
		Email:               email,
		PasswordHash:        string(passwordHash),
		RecoveryCodeHash:    string(recoveryHash),
		LocalAuthEnabled:    true,
		AuthSessionVersion:  1,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         models.DefaultCycleLength,
		PeriodLength:        models.DefaultPeriodLength,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return user
}

func readPasswordResetTwoOwnerUser(t *testing.T, database *gorm.DB, userID uint) models.User {
	t.Helper()

	var user models.User
	if err := database.First(&user, userID).Error; err != nil {
		t.Fatalf("read user %d: %v", userID, err)
	}
	return user
}
