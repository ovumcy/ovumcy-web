package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
)

// TestResetPasswordCASMissPropagatesTheSharedAlreadyConsumedSentinel drives a
// CAS miss through the REAL user repository and asserts that what comes back
// out of the service still satisfies errors.Is against the sentinel a
// services- or api-level caller would test.
//
// The stub in password_reset_cas_regression_test.go returns the services-level
// value from its own fake repository, so it agrees with itself no matter what
// the db layer raises; only the real repository can show whether the value that
// crosses the layer boundary is the value callers compare against.
//
// Both spellings are asserted on purpose. db.ErrResetTokenAlreadyConsumed is
// what the repository raises and ErrResetTokenAlreadyConsumed is what a caller
// in this package reaches for; the invariant is that they are one value, so a
// future change that re-splits them fails here whichever side it re-declares.
func TestResetPasswordCASMissPropagatesTheSharedAlreadyConsumedSentinel(t *testing.T) {
	database := newTwoOwnerIntegrationDatabase(t, "ovumcy-reset-sentinel-identity")
	service := NewAuthService(db.NewUserRepository(database))

	owner := createTwoOwnerUser(t, database, "reset-sentinel@example.com", withLocalCredentials(t, "OwnerPass1"))
	target := readTwoOwnerUser(t, database, owner.ID)

	// A stale oldPasswordHash is exactly the state a replayed or concurrent
	// redeem reaches the UPDATE in: the CAS predicate matches 0 rows.
	_, err := service.ResetPasswordAndRotateRecoveryCodeCAS(context.Background(), &target, "not-the-stored-hash", "EvenStronger2")
	if err == nil {
		t.Fatal("expected a CAS miss on a stale password hash, got no error")
	}
	if !errors.Is(err, db.ErrResetTokenAlreadyConsumed) {
		t.Fatalf("CAS miss did not match the db-layer sentinel it is raised from: got %v", err)
	}
	if !errors.Is(err, ErrResetTokenAlreadyConsumed) {
		t.Fatalf("CAS miss did not match the services-level sentinel callers test against: got %v (%q) — the two layers declare separate values with identical text, so errors.Is is false for the very error being tested for", err, err.Error())
	}

	// Positive anchor: the same call on the CURRENT hash must succeed, or the
	// assertions above would hold on a service that fails every reset.
	fresh := readTwoOwnerUser(t, database, owner.ID)
	if _, err := service.ResetPasswordAndRotateRecoveryCodeCAS(context.Background(), &fresh, fresh.PasswordHash, "EvenStronger2"); err != nil {
		t.Fatalf("expected the reset to succeed against the current password hash: %v", err)
	}
}
