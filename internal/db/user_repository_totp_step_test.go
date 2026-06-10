package db

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func openTOTPStepRepoForTest(t *testing.T) *UserRepository {
	t.Helper()
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "totp-step.db"))
	return NewUserRepository(database)
}

func createUserForTOTPTest(t *testing.T, repo *UserRepository, email string) models.User {
	t.Helper()
	user := models.User{
		Email:               email,
		PasswordHash:        "hash",
		RecoveryCodeHash:    "recovery",
		Role:                models.RoleOwner,
		LocalAuthEnabled:    true,
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), &user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func TestClaimTOTPStepFirstClaim(t *testing.T) {
	repo := openTOTPStepRepoForTest(t)
	user := createUserForTOTPTest(t, repo, "totp-first@example.com")

	ok, err := repo.ClaimTOTPStep(context.Background(), user.ID, 1000)
	if err != nil {
		t.Fatalf("ClaimTOTPStep() error: %v", err)
	}
	if !ok {
		t.Fatal("expected first claim to succeed")
	}

	var reloaded models.User
	if err := repo.database.First(&reloaded, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.TOTPLastUsedStep != 1000 {
		t.Fatalf("expected totp_last_used_step=1000, got %d", reloaded.TOTPLastUsedStep)
	}
}

func TestClaimTOTPStepReplay(t *testing.T) {
	repo := openTOTPStepRepoForTest(t)
	user := createUserForTOTPTest(t, repo, "totp-replay@example.com")

	if ok, err := repo.ClaimTOTPStep(context.Background(), user.ID, 1000); err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}

	ok, err := repo.ClaimTOTPStep(context.Background(), user.ID, 1000)
	if err != nil {
		t.Fatalf("replay claim error: %v", err)
	}
	if ok {
		t.Fatal("expected replay of same step to fail")
	}

	var reloaded models.User
	if err := repo.database.First(&reloaded, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.TOTPLastUsedStep != 1000 {
		t.Fatalf("expected totp_last_used_step unchanged at 1000, got %d", reloaded.TOTPLastUsedStep)
	}
}

func TestClaimTOTPStepOlderStep(t *testing.T) {
	repo := openTOTPStepRepoForTest(t)
	user := createUserForTOTPTest(t, repo, "totp-older@example.com")

	if ok, err := repo.ClaimTOTPStep(context.Background(), user.ID, 2000); err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}

	ok, err := repo.ClaimTOTPStep(context.Background(), user.ID, 1000)
	if err != nil {
		t.Fatalf("older step claim error: %v", err)
	}
	if ok {
		t.Fatal("expected claim of older step to fail")
	}
}

func TestClaimTOTPStepNewerStep(t *testing.T) {
	repo := openTOTPStepRepoForTest(t)
	user := createUserForTOTPTest(t, repo, "totp-newer@example.com")

	if ok, err := repo.ClaimTOTPStep(context.Background(), user.ID, 1000); err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}

	ok, err := repo.ClaimTOTPStep(context.Background(), user.ID, 2000)
	if err != nil {
		t.Fatalf("newer step claim error: %v", err)
	}
	if !ok {
		t.Fatal("expected claim of newer step to succeed")
	}

	var reloaded models.User
	if err := repo.database.First(&reloaded, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.TOTPLastUsedStep != 2000 {
		t.Fatalf("expected totp_last_used_step=2000, got %d", reloaded.TOTPLastUsedStep)
	}
}

func TestClaimTOTPStepUnknownUser(t *testing.T) {
	repo := openTOTPStepRepoForTest(t)

	ok, err := repo.ClaimTOTPStep(context.Background(), 99999, 1000)
	if err != nil {
		t.Fatalf("expected no error for nonexistent user, got %v", err)
	}
	if ok {
		t.Fatal("expected false for nonexistent user")
	}
}

func TestClaimTOTPStepConcurrentOneWinner(t *testing.T) {
	repo := openTOTPStepRepoForTest(t)
	user := createUserForTOTPTest(t, repo, "totp-concurrent@example.com")

	const goroutines = 5
	results := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		i := i
		go func() {
			defer wg.Done()
			ok, err := repo.ClaimTOTPStep(context.Background(), user.ID, 1000)
			if err != nil {
				t.Errorf("goroutine %d: ClaimTOTPStep error: %v", i, err)
			}
			results[i] = ok
		}()
	}
	wg.Wait()

	winners := 0
	for _, ok := range results {
		if ok {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly 1 winner among %d concurrent claims, got %d", goroutines, winners)
	}
}
