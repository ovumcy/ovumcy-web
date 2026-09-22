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

// TestUserRepositoryCompleteOnboardingRefusesZeroOwner proves CompleteOnboarding
// refuses a zero userID rather than running its writes: Where("id = ?", 0) /
// Where("user_id = ?", 0) ordinarily matches zero rows and returns nil, a
// silent no-op indistinguishable from a completed onboarding.
func TestUserRepositoryCompleteOnboardingRefusesZeroOwner(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)

	err := repo.CompleteOnboarding(context.Background(), 0, time.Now().UTC(), 5, false)
	if err == nil || err.Error() != "onboarding owner is required" {
		t.Fatalf("expected the owner-required refusal, got %v", err)
	}
}

// TestUserRepositoryCompleteOnboardingScopesDailyLogUpdateToOwner proves the
// per-day update inside CompleteOnboarding's auto-fill loop is scoped by
// user_id in the query itself, mirroring DailyLogRepository.Save. The entry is
// read scoped by userID in the same transaction, so today the update only
// ever reaches the owner's own row (defense-in-depth) — this pins the guard
// rather than a currently-reachable cross-owner write.
func TestUserRepositoryCompleteOnboardingScopesDailyLogUpdateToOwner(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)
	ctx := context.Background()

	owner := &models.User{
		Email:            "onboarding-owner@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repo.Create(ctx, owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}

	startDay := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	existing := models.DailyLog{
		UserID:        owner.ID,
		Date:          startDay,
		IsPeriod:      false,
		Flow:          models.FlowNone,
		SexActivity:   models.SexActivityNone,
		CervicalMucus: models.CervicalMucusNone,
		PregnancyTest: models.PregnancyTestNone,
		SymptomIDs:    []uint{},
	}
	if err := repo.database.WithContext(ctx).Create(&existing).Error; err != nil {
		t.Fatalf("seed existing day: %v", err)
	}

	if err := repo.CompleteOnboarding(ctx, owner.ID, startDay, 1, true); err != nil {
		t.Fatalf("complete onboarding: %v", err)
	}

	var reloaded models.DailyLog
	if err := repo.database.WithContext(ctx).First(&reloaded, existing.ID).Error; err != nil {
		t.Fatalf("reload day: %v", err)
	}
	if !reloaded.IsPeriod {
		t.Fatal("expected the onboarding auto-fill to mark the existing day as period")
	}
}
