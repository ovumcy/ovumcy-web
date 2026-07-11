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

	// Reload from the DB and assert the persisted effect — a silent no-op must fail.
	var reloaded models.DailyLog
	if err := database.First(&reloaded, entry.ID).Error; err != nil {
		t.Fatalf("reload daily log: %v", err)
	}
	if len(reloaded.SymptomIDs) != 3 || reloaded.SymptomIDs[0] != 1 || reloaded.SymptomIDs[1] != 2 || reloaded.SymptomIDs[2] != 3 {
		t.Fatalf("UpdateSymptomIDs did not persist symptom_ids, got %v want [1 2 3]", reloaded.SymptomIDs)
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

	touchedAt := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	if err := identityRepo.TouchLastUsed(context.Background(), identity.ID, touchedAt); err != nil {
		t.Fatalf("TouchLastUsed() unexpected error: %v", err)
	}

	// Reload and assert the timestamp persisted — a silent no-op leaves it nil.
	var reloaded models.OIDCIdentity
	if err := database.First(&reloaded, identity.ID).Error; err != nil {
		t.Fatalf("reload identity: %v", err)
	}
	if reloaded.LastUsedAt == nil {
		t.Fatal("TouchLastUsed did not persist last_used_at (still nil)")
	}
	if reloaded.LastUsedAt.Before(touchedAt) {
		t.Fatalf("last_used_at %v is before the touched timestamp %v", reloaded.LastUsedAt.UTC(), touchedAt)
	}

	// id == 0 is a no-op guard that must not touch the database.
	if err := identityRepo.TouchLastUsed(context.Background(), 0, time.Now().UTC()); err != nil {
		t.Fatalf("TouchLastUsed(0) should be a no-op, got %v", err)
	}
}
