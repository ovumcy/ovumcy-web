package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// The owner's account erasure is a revoke-or-erase action gated on a fresh
// re-auth, so it runs only from the session version that re-auth was verified
// against — the compare-and-set clear-data already applies (WEB-395 L4). A
// revocation committed in between (a password change, a sign-out everywhere)
// must leave the account standing.
func TestDeleteAccountFromAuthSessionVersionErasesOnlyFromTheVerifiedVersion(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		database, err := OpenDatabase(migratedSQLiteConfig(t, filepath.Join(t.TempDir(), "delete-account-cas.db")))
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		t.Cleanup(func() {
			if sqlDB, err := database.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		assertDeleteAccountErasesOnlyFromTheVerifiedVersion(t, database)
	})

	t.Run("postgres", func(t *testing.T) {
		assertDeleteAccountErasesOnlyFromTheVerifiedVersion(t, openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t)))
	})
}

func assertDeleteAccountErasesOnlyFromTheVerifiedVersion(t *testing.T, database *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repo := NewUserRepository(database)
	fixture := &sessionVersionFixture{t: t, database: database}

	revoked := fixture.seed(4)
	if err := repo.DeleteAccountFromAuthSessionVersion(ctx, revoked, 3); !errors.Is(err, models.ErrAuthSessionVersionChanged) {
		t.Fatalf("expected ErrAuthSessionVersionChanged for a version revoked since the re-auth, got %v", err)
	}
	if version, _ := fixture.read(revoked, "email"); version != 4 {
		t.Fatalf("a refused erasure must leave the account at its stored version 4, got %d", version)
	}
	if remaining := countWhere(t, database, &models.User{}, "id = ?", revoked); remaining != 1 {
		t.Fatalf("a refused erasure must leave the account in place, %d row(s) remain", remaining)
	}

	verified := fixture.seed(3)
	if err := repo.DeleteAccountFromAuthSessionVersion(ctx, verified, 3); err != nil {
		t.Fatalf("erasure from the verified version: %v", err)
	}
	if remaining := countWhere(t, database, &models.User{}, "id = ?", verified); remaining != 0 {
		t.Fatalf("erasure from the verified version must remove the account, %d row(s) remain", remaining)
	}

	if err := repo.DeleteAccountFromAuthSessionVersion(ctx, verified, 3); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("expected ErrUserOwnerRequired for an account already erased, got %v", err)
	}
	if err := repo.DeleteAccountFromAuthSessionVersion(ctx, 0, 1); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("expected ErrUserOwnerRequired for a zero owner, got %v", err)
	}
}

// The operator reset must not report success for an account that no longer
// exists: the CLI looks the account up first, and a deletion landing between
// that lookup and this write would otherwise read as a completed reset
// (WEB-395 L9).
func TestForceResetPasswordRefusesAnAccountThatNoLongerExists(t *testing.T) {
	database, err := OpenDatabase(migratedSQLiteConfig(t, filepath.Join(t.TempDir(), "force-reset-vanished.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repo := NewUserRepository(database)
	if err := repo.ForceResetPasswordAndRevokeSessions(context.Background(), 99999, "operator-hash"); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("expected ErrUserOwnerRequired for a vanished account, got %v", err)
	}
}
