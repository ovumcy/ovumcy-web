package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestOpenSQLiteCreatesCaseInsensitiveUserEmailUniqueIndex(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-email-index.db")
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	firstUser := models.User{
		Email:        "QA-Test2@Ovumcy.Local",
		PasswordHash: "hash-1",
		Role:         models.RoleOwner,
		CycleLength:  models.DefaultCycleLength,
		PeriodLength: models.DefaultPeriodLength,
		CreatedAt:    time.Now().UTC(),
	}
	if err := database.Create(&firstUser).Error; err != nil {
		t.Fatalf("create first user: %v", err)
	}

	secondUser := models.User{
		Email:        "qa-test2@ovumcy.local",
		PasswordHash: "hash-2",
		Role:         models.RoleOwner,
		CycleLength:  models.DefaultCycleLength,
		PeriodLength: models.DefaultPeriodLength,
		CreatedAt:    time.Now().UTC(),
	}
	if err := database.Create(&secondUser).Error; err == nil {
		t.Fatalf("expected duplicate normalized email insert to fail")
	}
}
