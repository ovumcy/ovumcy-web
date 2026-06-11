package db

// method_coverage_test.go — exercises repository methods that no unit or
// integration test reached (patch-coverage gaps after the context-threading
// refactor). Each test drives the method against a migrated SQLite database.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

func openMethodCoverageDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := OpenSQLite(filepath.Join(t.TempDir(), "method-coverage.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return database
}

func seedMethodCoverageUser(t *testing.T, repo *UserRepository) *models.User {
	t.Helper()
	user := &models.User{Email: "owner@example.com", PasswordHash: "hash", Role: "owner"}
	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return user
}

func TestUserRepositorySavePersistsChanges(t *testing.T) {
	repo := NewUserRepository(openMethodCoverageDB(t))
	user := seedMethodCoverageUser(t, repo)

	user.DisplayName = "Renamed"
	if err := repo.Save(context.Background(), user); err != nil {
		t.Fatalf("Save() unexpected error: %v", err)
	}

	stored, err := repo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("FindByID() error: %v", err)
	}
	if stored.DisplayName != "Renamed" {
		t.Fatalf("Save did not persist display name, got %q", stored.DisplayName)
	}
}

func TestUserRepositoryUpdateTOTPSecretCiphertext(t *testing.T) {
	repo := NewUserRepository(openMethodCoverageDB(t))
	user := seedMethodCoverageUser(t, repo)

	if err := repo.UpdateTOTPSecretCiphertext(context.Background(), user.ID, "new-ciphertext"); err != nil {
		t.Fatalf("UpdateTOTPSecretCiphertext() unexpected error: %v", err)
	}

	stored, err := repo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("FindByID() error: %v", err)
	}
	if stored.TOTPSecret != "new-ciphertext" {
		t.Fatalf("totp secret not updated, got %q", stored.TOTPSecret)
	}
}

func TestDailyLogRepositoryUpdateSymptomIDsAndTransaction(t *testing.T) {
	database := openMethodCoverageDB(t)
	user := seedMethodCoverageUser(t, NewUserRepository(database))
	logRepo := NewDailyLogRepository(database)

	entry := &models.DailyLog{UserID: user.ID, Date: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}
	if err := logRepo.Create(context.Background(), entry); err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	entry.SymptomIDs = []uint{1, 2, 3}
	if err := logRepo.UpdateSymptomIDs(context.Background(), entry); err != nil {
		t.Fatalf("UpdateSymptomIDs() unexpected error: %v", err)
	}

	// WithinTransaction must commit a write made through the tx-scoped repository.
	err := logRepo.WithinTransaction(context.Background(), func(tx *DailyLogRepository) error {
		return tx.Create(context.Background(), &models.DailyLog{UserID: user.ID, Date: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)})
	})
	if err != nil {
		t.Fatalf("WithinTransaction() unexpected error: %v", err)
	}
}

func TestOIDCIdentityRepositoryTouchLastUsed(t *testing.T) {
	database := openMethodCoverageDB(t)
	user := seedMethodCoverageUser(t, NewUserRepository(database))
	identityRepo := NewOIDCIdentityRepository(database)

	identity := &models.OIDCIdentity{UserID: user.ID, Issuer: "https://id.example.com", Subject: "subj", CreatedAt: time.Now().UTC()}
	if err := identityRepo.Create(context.Background(), identity); err != nil {
		t.Fatalf("create identity: %v", err)
	}

	if err := identityRepo.TouchLastUsed(context.Background(), identity.ID, time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("TouchLastUsed() unexpected error: %v", err)
	}

	// id == 0 is a no-op guard that must not touch the database.
	if err := identityRepo.TouchLastUsed(context.Background(), 0, time.Now().UTC()); err != nil {
		t.Fatalf("TouchLastUsed(0) should be a no-op, got %v", err)
	}
}
