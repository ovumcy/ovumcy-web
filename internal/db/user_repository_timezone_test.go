package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func openTimezoneRepoForTest(t *testing.T) *UserRepository {
	t.Helper()
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "timezone.db"))
	return NewUserRepository(database)
}

func createUserForTimezoneTest(t *testing.T, repo *UserRepository, email string) models.User {
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

func reloadUserTimezone(t *testing.T, repo *UserRepository, userID uint) string {
	t.Helper()
	var reloaded models.User
	if err := repo.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded.Timezone
}

func TestUpdateUserTimezonePersistsValue(t *testing.T) {
	repo := openTimezoneRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "tz-persist@example.com")

	if got := reloadUserTimezone(t, repo, user.ID); got != "" {
		t.Fatalf("expected empty timezone on fresh user, got %q", got)
	}

	if err := repo.UpdateUserTimezone(context.Background(), user.ID, "Europe/Belgrade"); err != nil {
		t.Fatalf("UpdateUserTimezone: %v", err)
	}

	if got := reloadUserTimezone(t, repo, user.ID); got != "Europe/Belgrade" {
		t.Fatalf("expected persisted timezone Europe/Belgrade, got %q", got)
	}
}

// TestUpdateUserTimezoneScopedToUser proves the write is strictly scoped to the
// target user id: writing owner A's timezone never touches owner B's row (the
// household-multi-owner isolation boundary).
func TestUpdateUserTimezoneScopedToUser(t *testing.T) {
	repo := openTimezoneRepoForTest(t)
	owner := createUserForTimezoneTest(t, repo, "tz-owner@example.com")
	other := createUserForTimezoneTest(t, repo, "tz-other@example.com")

	if err := repo.UpdateUserTimezone(context.Background(), other.ID, "Asia/Tokyo"); err != nil {
		t.Fatalf("seed other owner timezone: %v", err)
	}

	if err := repo.UpdateUserTimezone(context.Background(), owner.ID, "America/Toronto"); err != nil {
		t.Fatalf("UpdateUserTimezone owner: %v", err)
	}

	if got := reloadUserTimezone(t, repo, owner.ID); got != "America/Toronto" {
		t.Fatalf("expected owner timezone America/Toronto, got %q", got)
	}
	if got := reloadUserTimezone(t, repo, other.ID); got != "Asia/Tokyo" {
		t.Fatalf("expected other owner timezone untouched at Asia/Tokyo, got %q", got)
	}
}

func TestUpdateUserTimezoneUnknownUserIsNoop(t *testing.T) {
	repo := openTimezoneRepoForTest(t)

	if err := repo.UpdateUserTimezone(context.Background(), 99999, "Europe/Belgrade"); err != nil {
		t.Fatalf("expected no error updating nonexistent user, got %v", err)
	}
}
