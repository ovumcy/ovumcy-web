package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/db"
	"github.com/terraincognita07/ovumcy/internal/models"
	"gorm.io/gorm"
)

func newDataAccessTestHandler(t *testing.T) (*Handler, *gorm.DB) {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "ovumcy-data-access-test.db")
	database, err := db.OpenSQLite(databasePath)
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

	return newServiceBackedHandlerForTest(database, time.UTC), database
}

func newServiceBackedHandlerForTest(database *gorm.DB, location *time.Location) *Handler {
	if location == nil {
		location = time.UTC
	}

	handler := &Handler{
		location: location,
	}
	return handler.withDependencies(newTestHandlerDependencies(database))
}

func createDataAccessTestUser(t *testing.T, database *gorm.DB, email string) models.User {
	t.Helper()

	user := models.User{
		Email:               email,
		PasswordHash:        "test-hash",
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}
