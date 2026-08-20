package services

import (
	"context"
	"reflect"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
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
	database := newTwoOwnerIntegrationDatabase(t, "ovumcy-reset-two-owner")
	service := NewAuthService(db.NewUserRepository(database))

	ownerA := createTwoOwnerUser(t, database, "reset-owner-a@example.com", withLocalCredentials(t, "OwnerAPass1"))
	ownerB := createTwoOwnerUser(t, database, "reset-owner-b@example.com", withLocalCredentials(t, "OwnerBPass1"))
	requireDistinctTwoOwnerFixture(t, ownerA, ownerB)

	before := readTwoOwnerUser(t, database, ownerA.ID)

	target := readTwoOwnerUser(t, database, ownerB.ID)
	recoveryCode, err := service.ResetPasswordAndRotateRecoveryCodeCAS(context.Background(), &target, target.PasswordHash, "EvenStronger2")
	if err != nil {
		t.Fatalf("resetting owner B failed with %v — a CAS scoped to a different owner matches no row", err)
	}
	if recoveryCode == "" {
		t.Fatal("expected a rotated recovery code for owner B")
	}

	// Isolation: owner A must not have been touched in any column.
	after := readTwoOwnerUser(t, database, ownerA.ID)
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
	gotB := readTwoOwnerUser(t, database, ownerB.ID)
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

// withLocalCredentials gives a seeded owner a real bcrypt password and recovery
// hash, so the CAS predicate compares the same shape production does. MinCost
// keeps two seeds plus the service's own hashing off the critical path.
func withLocalCredentials(t *testing.T, password string) func(*models.User) {
	t.Helper()

	return func(user *models.User) {
		passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("hash password for %s: %v", user.Email, err)
		}
		recoveryHash, err := bcrypt.GenerateFromPassword([]byte("recovery-"+user.Email), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("hash recovery code for %s: %v", user.Email, err)
		}
		user.PasswordHash = string(passwordHash)
		user.RecoveryCodeHash = string(recoveryHash)
		user.LocalAuthEnabled = true
		user.AuthSessionVersion = 1
	}
}
