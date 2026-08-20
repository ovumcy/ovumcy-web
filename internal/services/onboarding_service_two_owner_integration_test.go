package services

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// TestOnboardingServiceScopesEveryWriteToTheCallingOwner drives all four
// owner-carrying onboarding seams — SaveStep1, SaveStep2, the completion
// baseline read, and CompleteOnboarding — through the REAL user repository with
// two independent owners in one database.
//
// The stub-level assertions elsewhere in this package observe which id the
// service forwards; they cannot observe what that id does to a second account.
// This test can: owner A is created first, so it holds id 1 — the value a
// dropped or hard-coded owner id degenerates to — and every operation here runs
// against owner B. Owner A must come out byte-for-byte unchanged, with no
// auto-filled day rows of its own.
func TestOnboardingServiceScopesEveryWriteToTheCallingOwner(t *testing.T) {
	database := newOnboardingTwoOwnerDatabase(t)
	repositories := db.NewRepositories(database)
	service := NewOnboardingService(repositories.Users)

	ownerA := createOnboardingTwoOwnerUser(t, database, "onboarding-owner-a@example.com")
	ownerB := createOnboardingTwoOwnerUser(t, database, "onboarding-owner-b@example.com")

	// The mutant this test exists to kill is a literal owner `1`. If the first
	// account did not land on id 1 the whole test would go green while proving
	// nothing, so pin the fixture assumption rather than trusting it.
	if ownerA.ID != 1 {
		t.Fatalf("fixture assumption broken: expected the first owner to hold id 1, got %d", ownerA.ID)
	}
	if ownerB.ID == ownerA.ID {
		t.Fatalf("fixture assumption broken: the two owners must be distinct, both are %d", ownerB.ID)
	}

	before := readOnboardingTwoOwnerUser(t, database, ownerA.ID)

	start := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	if err := service.SaveStep1(context.Background(), ownerB.ID, start); err != nil {
		t.Fatalf("SaveStep1() unexpected error: %v", err)
	}
	if _, _, err := service.SaveStep2(context.Background(), ownerB.ID, 31, 6, true, true, models.UsageGoalAvoid); err != nil {
		t.Fatalf("SaveStep2() unexpected error: %v", err)
	}
	startDay, err := service.CompleteOnboardingForUser(context.Background(), ownerB.ID, time.UTC)
	if err != nil {
		t.Fatalf("CompleteOnboardingForUser() unexpected error: %v", err)
	}
	if !startDay.Equal(start) {
		t.Fatalf("expected completion to anchor on owner B's step-1 date %s, got %s", start, startDay)
	}

	// Isolation: nothing owner B did may reach owner A.
	after := readOnboardingTwoOwnerUser(t, database, ownerA.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("owner A's row changed while owner B onboarded — an onboarding write is not scoped to the calling owner:\nbefore: %+v\nafter:  %+v", before, after)
	}
	if days := countOnboardingTwoOwnerDays(t, database, ownerA.ID); days != 0 {
		t.Fatalf("expected owner A to keep zero day rows, got %d", days)
	}

	// Positive anchor: the writes must actually have landed on owner B, or the
	// isolation half above would pass on a service that writes nothing at all.
	gotB := readOnboardingTwoOwnerUser(t, database, ownerB.ID)
	if !gotB.OnboardingCompleted {
		t.Fatal("expected owner B to be marked onboarded")
	}
	if gotB.CycleLength != 31 || gotB.PeriodLength != 6 {
		t.Fatalf("expected owner B baseline 31/6, got %d/%d", gotB.CycleLength, gotB.PeriodLength)
	}
	if gotB.UsageGoal != models.UsageGoalAvoid || !gotB.IrregularCycle {
		t.Fatalf("expected owner B step-2 preferences persisted, got usage_goal=%q irregular=%v", gotB.UsageGoal, gotB.IrregularCycle)
	}
	if gotB.LastPeriodStart == nil || !gotB.LastPeriodStart.Equal(start) {
		t.Fatalf("expected owner B last_period_start %s, got %v", start, gotB.LastPeriodStart)
	}
	if days := countOnboardingTwoOwnerDays(t, database, ownerB.ID); days != 6 {
		t.Fatalf("expected 6 auto-filled period days for owner B, got %d", days)
	}
}

func newOnboardingTwoOwnerDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{
		Driver:     db.DriverSQLite,
		SQLitePath: filepath.Join(t.TempDir(), "ovumcy-onboarding-two-owner.db"),
	})
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return database
}

func createOnboardingTwoOwnerUser(t *testing.T, database *gorm.DB, email string) models.User {
	t.Helper()

	user := models.User{
		Email:               email,
		PasswordHash:        "test-hash",
		Role:                models.RoleOwner,
		OnboardingCompleted: false,
		CycleLength:         models.DefaultCycleLength,
		PeriodLength:        models.DefaultPeriodLength,
		AutoPeriodFill:      true,
		UsageGoal:           models.UsageGoalHealth,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return user
}

func readOnboardingTwoOwnerUser(t *testing.T, database *gorm.DB, userID uint) models.User {
	t.Helper()

	var user models.User
	if err := database.First(&user, userID).Error; err != nil {
		t.Fatalf("read user %d: %v", userID, err)
	}
	return user
}

func countOnboardingTwoOwnerDays(t *testing.T, database *gorm.DB, userID uint) int64 {
	t.Helper()

	var count int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count day rows for user %d: %v", userID, err)
	}
	return count
}
