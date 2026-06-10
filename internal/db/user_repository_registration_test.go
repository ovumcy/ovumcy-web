package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func openRegistrationRepositoryForTest(t *testing.T) *UserRepository {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "registration-repository.db")
	database, err := OpenSQLite(databasePath)
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

	return NewUserRepository(database)
}

func TestUserRepositoryCreateTranslatesUniqueViolation(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)

	first := &models.User{
		Email:            "unique@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), first); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	second := &models.User{
		Email:            "unique@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	err := repo.Create(context.Background(), second)
	if err == nil {
		t.Fatal("expected unique violation error")
	}

	var uniqueErr *UniqueConstraintError
	if !errors.As(err, &uniqueErr) {
		t.Fatalf("expected UniqueConstraintError, got %T %v", err, err)
	}
}

func TestUserRepositoryCreateUserWithSymptomsRollsBackOnSeedFailure(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)

	if err := repo.database.Exec("DROP TABLE symptom_types").Error; err != nil {
		t.Fatalf("drop symptom_types: %v", err)
	}

	user := &models.User{
		Email:            "rollback@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	symptoms := []models.SymptomType{{
		Name:      "Test",
		Icon:      "✨",
		Color:     "#111111",
		IsBuiltin: true,
	}}

	err := repo.CreateUserWithSymptoms(context.Background(), user, symptoms)
	if err == nil {
		t.Fatal("expected seed write error")
	}

	var seedErr *SymptomSeedError
	if !errors.As(err, &seedErr) {
		t.Fatalf("expected SymptomSeedError, got %T %v", err, err)
	}

	exists, checkErr := repo.ExistsByNormalizedEmail(context.Background(), "rollback@example.com")
	if checkErr != nil {
		t.Fatalf("check rollback user existence: %v", checkErr)
	}
	if exists {
		t.Fatal("expected user insert rollback on symptom seed failure")
	}
}
