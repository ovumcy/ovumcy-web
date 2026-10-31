package db

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestSQLiteErasureLeavesNoRemnantInTheFile: an erased day must not stay
// readable in the database file. Without secure_delete SQLite only unlinks the
// deleted cells, and their bytes — the owner's notes — survive in free pages
// and cell slack until something reuses them.
func TestSQLiteErasureLeavesNoRemnantInTheFile(t *testing.T) {
	erasures := map[string]func(*UserRepository, models.User) error{
		"clear-data": func(repo *UserRepository, user models.User) error {
			return repo.ClearAllDataAndResetSettings(context.Background(), user.ID, user.AuthSessionVersion)
		},
		"account deletion": func(repo *UserRepository, user models.User) error {
			return repo.DeleteAccountAndRelatedData(context.Background(), user.ID)
		},
	}
	for name, erase := range erasures {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "erasure.db")
			database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: path})
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			sqlDB, err := database.DB()
			if err != nil {
				t.Fatalf("sql.DB: %v", err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })

			repos := NewRepositories(database)
			ctx := context.Background()
			user := &models.User{
				Email:            "erasure-remnant@example.com",
				PasswordHash:     "hash",
				RecoveryCodeHash: "recovery",
				Role:             models.RoleOwner,
				CycleLength:      models.DefaultCycleLength,
				PeriodLength:     models.DefaultPeriodLength,
				CreatedAt:        time.Now().UTC(),
			}
			if err := repos.Users.Create(ctx, user); err != nil {
				t.Fatalf("create user: %v", err)
			}
			const marker = "erased-note-remnant-marker"
			start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
			for day := range 40 {
				if err := repos.DailyLogs.Create(ctx, &models.DailyLog{
					UserID: user.ID, Date: start.AddDate(0, 0, day), IsPeriod: true,
					Notes: fmt.Sprintf("%s %02d", marker, day), CycleFactorKeys: []string{},
				}); err != nil {
					t.Fatalf("create day %d: %v", day, err)
				}
			}
			stored, err := repos.Users.FindByID(ctx, user.ID)
			if err != nil {
				t.Fatalf("reload user: %v", err)
			}

			if err := erase(repos.Users, stored); err != nil {
				t.Fatalf("erase: %v", err)
			}
			if err := database.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
				t.Fatalf("checkpoint: %v", err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			for _, file := range []string{path, path + "-wal"} {
				content, err := os.ReadFile(file)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					t.Fatalf("read %s: %v", filepath.Base(file), err)
				}
				if bytes.Contains(content, []byte(marker)) {
					t.Fatalf("%s still holds an erased day's note", filepath.Base(file))
				}
			}
		})
	}
}
