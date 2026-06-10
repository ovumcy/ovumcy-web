package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestSQLitePragmasEnforced is the regression for the DSN-form bug: the
// glebarez/modernc driver ignores mattn-style `_foreign_keys=on` params, so the
// previous DSN left foreign_keys OFF and the journal in rollback mode on every
// pooled connection. This test opens the database through the real production
// opener and asserts that foreign_keys is enforced and the journal is WAL, then
// proves ON DELETE CASCADE actually fires by deleting a user row directly (NOT
// through the repository's explicit child-row deletes) and checking the child
// daily_logs row is gone.
func TestSQLitePragmasEnforced(t *testing.T) {
	dir := t.TempDir()
	database, err := OpenSQLite(filepath.Join(dir, "pragma.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	var foreignKeys int
	if err := database.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("read PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1 (FK enforcement must be ON for every pooled connection)", foreignKeys)
	}

	var journalMode string
	if err := database.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read PRAGMA journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	repos := NewRepositories(database)
	user := &models.User{
		Email:            "pragma@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repos.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	dayLog := &models.DailyLog{
		UserID:   user.ID,
		Date:     time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
		IsPeriod: true,
	}
	if err := repos.DailyLogs.Create(context.Background(), dayLog); err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	// Delete the parent user row directly — bypassing the repository helper that
	// deletes child rows explicitly — so the only thing that can remove the
	// child daily_log is ON DELETE CASCADE, which requires foreign_keys=ON.
	if err := database.Exec("DELETE FROM users WHERE id = ?", user.ID).Error; err != nil {
		t.Fatalf("delete user row: %v", err)
	}

	var orphans int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ?", user.ID).Count(&orphans).Error; err != nil {
		t.Fatalf("count orphaned logs: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("ON DELETE CASCADE did not fire: %d orphaned daily_logs remain after parent delete (foreign_keys not enforced on the pooled connection)", orphans)
	}
}
