package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("expected ErrUserOwnerRequired, got %v", err)
	}
}

// TestUserRepositoryCompleteOnboardingMarksAnExistingDayAsPeriod covers the
// auto-fill loop's update arm: a day row that already exists on an onboarding
// date is updated rather than inserted a second time.
//
// It deliberately does NOT cover the user_id predicate on that update. The
// entry is read scoped by userID in the same transaction, so removing the
// predicate changes nothing this test can observe and it stays green either
// way — the predicate is pinned by
// TestUserRepositoryCompleteOnboardingUpdateIsScopedByOwnerInSource below, and
// that test is not redundant with this one.
func TestUserRepositoryCompleteOnboardingMarksAnExistingDayAsPeriod(t *testing.T) {
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

// TestUserRepositoryCompleteOnboardingUpdateIsScopedByOwnerInSource pins the
// user_id predicate on CompleteOnboarding's per-day Updates call. The entry
// is read scoped by userID earlier in the same transaction, so today this
// predicate never changes which row is touched — it is defense-in-depth, not
// a currently-reachable cross-owner write, and no behavioral test can force a
// different outcome by removing it. This structural check is what catches a
// regression back to the primary-key-only Updates that DailyLogRepository.Save
// documents the same risk for.
func TestUserRepositoryCompleteOnboardingUpdateIsScopedByOwnerInSource(t *testing.T) {
	body := userRepositoryFunctionBody(t, "func (repo *UserRepository) CompleteOnboarding(")

	if !strings.Contains(body, `tx.Model(&entry).Where("user_id=?",userID).Updates(`) {
		t.Fatal("expected CompleteOnboarding's per-day Updates call to be scoped by " +
			`Where("user_id = ?", userID), mirroring DailyLogRepository.Save`)
	}
}

// TestUserRepositoryCreateUserWithSymptomsChecksSeedOwnersInSource pins the
// owner check on the one symptom insert in this package that does not go
// through SymptomRepository. CreateUserWithSymptoms stamps user.ID onto the
// seed rows and writes them with its own transaction handle, so the guard on
// SymptomRepository.Create/CreateBatch does not cover it; leaving it uncovered
// would fix the class at every site but this one, which is how the two halves
// drift apart. The id is assigned by the insert one statement earlier, so no
// behavioral test can drive this path to a zero owner — the check is pinned
// where it is written.
func TestUserRepositoryCreateUserWithSymptomsChecksSeedOwnersInSource(t *testing.T) {
	body := userRepositoryFunctionBody(t, "func (repo *UserRepository) CreateUserWithSymptoms(")

	check := strings.Index(body, "requireSymptomOwners(prepared)")
	insert := strings.Index(body, "tx.Create(&prepared)")
	if check < 0 {
		t.Fatal("expected CreateUserWithSymptoms to run requireSymptomOwners over the seed rows, " +
			"the same owner check SymptomRepository's own inserts run")
	}
	if insert < 0 {
		t.Fatal("seed insert not found in CreateUserWithSymptoms; update this guard")
	}
	if check > insert {
		t.Fatal("expected the owner check to run before the seed insert, not after it")
	}
}

// userRepositoryFunctionBody returns the source of one function in
// user_repository.go, with every run of whitespace collapsed away.
//
// The whitespace is dropped so a gofmt-legal reflow of a call chain — the
// kind a later edit to a neighbouring line can force — cannot fail a caller
// with a message claiming a security-shaped predicate was dropped while it is
// still there. Every whitespace run goes, string literals included, so a
// needle must be written with none at all: `Where("user_id=?",userID)`, not
// the spelling that appears in the source.
func userRepositoryFunctionBody(t *testing.T, signature string) string {
	t.Helper()

	source, err := os.ReadFile("user_repository.go")
	if err != nil {
		t.Fatalf("read the user repository source: %v", err)
	}
	body := string(source)

	start := strings.Index(body, signature)
	if start < 0 {
		t.Fatalf("function not found in the user repository source: %s", signature)
	}
	rest := body[start:]
	end := strings.Index(rest[1:], "\nfunc ")
	if end < 0 {
		end = len(rest)
	} else {
		end++
	}

	return strings.Join(strings.Fields(rest[:end]), "")
}
