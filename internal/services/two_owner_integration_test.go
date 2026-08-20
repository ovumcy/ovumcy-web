package services

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// Shared setup for this package's two-owner owner-scoping integration cases
// (onboarding, the password-reset CAS, OIDC identity resolution). Each of those
// seeds two independent owners on ONE database and drives the real repository
// as the later-created owner, so they need the same three things: a database,
// an owner row, and a re-read of an owner row.
//
// The config is separated from the setup exactly as in
// day_service_integration_test.go, so a Postgres variant of any of these cases
// is a five-line constructor over testdb.StartPostgresDSN rather than a fourth
// copy of the open/cleanup dance.

func newTwoOwnerIntegrationDatabase(t *testing.T, name string) *gorm.DB {
	t.Helper()

	return newTwoOwnerIntegrationDatabaseWithConfig(t, db.Config{
		Driver:     db.DriverSQLite,
		SQLitePath: filepath.Join(t.TempDir(), name+".db"),
	})
}

func newTwoOwnerIntegrationDatabaseWithConfig(t *testing.T, databaseConfig db.Config) *gorm.DB {
	t.Helper()

	database, err := db.OpenDatabase(databaseConfig)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return database
}

// createTwoOwnerUser seeds one owner. customize may adjust the row before the
// insert; the defaults are a plain onboarded owner with the shipped cycle
// baseline. The FIRST owner a case creates lands on id 1 — the value a dropped
// or hard-coded owner id degenerates to — which is what makes the second owner
// the acting one in every case below.
func createTwoOwnerUser(t *testing.T, database *gorm.DB, email string, customize func(*models.User)) models.User {
	t.Helper()

	user := models.User{
		Email:               email,
		PasswordHash:        "test-hash",
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         models.DefaultCycleLength,
		PeriodLength:        models.DefaultPeriodLength,
		AutoPeriodFill:      true,
		UsageGoal:           models.UsageGoalHealth,
		CreatedAt:           time.Now().UTC(),
	}
	if customize != nil {
		customize(&user)
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return user
}

func readTwoOwnerUser(t *testing.T, database *gorm.DB, userID uint) models.User {
	t.Helper()

	var user models.User
	if err := database.First(&user, userID).Error; err != nil {
		t.Fatalf("read user %d: %v", userID, err)
	}
	return user
}

// requireDistinctTwoOwnerFixture pins the fixture assumption every case here
// rests on: the mutant they exist to kill is a literal owner `1`, so the first
// owner must hold id 1 and the acting owner must be a different account. A
// fixture that drifted off either would leave the case green while proving
// nothing.
func requireDistinctTwoOwnerFixture(t *testing.T, first models.User, acting models.User) {
	t.Helper()

	if first.ID != 1 {
		t.Fatalf("fixture assumption broken: expected the first owner to hold id 1, got %d", first.ID)
	}
	if acting.ID == first.ID {
		t.Fatalf("fixture assumption broken: the two owners must be distinct, both are %d", acting.ID)
	}
}
