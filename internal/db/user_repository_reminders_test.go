package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func openReminderRepoForTest(t *testing.T) *UserRepository {
	t.Helper()
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "reminders.db"))
	return NewUserRepository(database)
}

func createUserForReminderTest(t *testing.T, repo *UserRepository, email string) models.User {
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

func reloadReminderLeadDays(t *testing.T, repo *UserRepository, userID uint) int {
	t.Helper()
	var reloaded models.User
	if err := repo.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded.ReminderLeadDays
}

func TestUpdateReminderLeadDaysPersistsValue(t *testing.T) {
	repo := openReminderRepoForTest(t)
	user := createUserForReminderTest(t, repo, "reminder-persist@example.com")

	if got := reloadReminderLeadDays(t, repo, user.ID); got != models.DefaultReminderLeadDays {
		t.Fatalf("expected default reminder_lead_days %d on fresh user, got %d", models.DefaultReminderLeadDays, got)
	}

	if err := repo.UpdateReminderLeadDays(context.Background(), user.ID, 10); err != nil {
		t.Fatalf("UpdateReminderLeadDays: %v", err)
	}

	if got := reloadReminderLeadDays(t, repo, user.ID); got != 10 {
		t.Fatalf("expected persisted reminder_lead_days 10, got %d", got)
	}
}

// TestLoadSettingsByIDReturnsReminderLeadDays proves the LoadSettingsByID Select
// whitelist includes reminder_lead_days — the settings render path reads it, so
// a missing column would render the control as its zero value (the
// show_historical_phases regression class).
func TestLoadSettingsByIDReturnsReminderLeadDays(t *testing.T) {
	repo := openReminderRepoForTest(t)
	user := createUserForReminderTest(t, repo, "reminder-load@example.com")

	if err := repo.UpdateReminderLeadDays(context.Background(), user.ID, 11); err != nil {
		t.Fatalf("UpdateReminderLeadDays: %v", err)
	}

	loaded, err := repo.LoadSettingsByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("LoadSettingsByID: %v", err)
	}
	if loaded.ReminderLeadDays != 11 {
		t.Fatalf("expected LoadSettingsByID to select reminder_lead_days=11, got %d", loaded.ReminderLeadDays)
	}
}

// TestUpdateReminderLeadDaysScopedToUser proves the write is strictly scoped to
// the target user id: writing owner A's lead window never touches owner B's row
// (the household-multi-owner isolation boundary).
func TestUpdateReminderLeadDaysScopedToUser(t *testing.T) {
	repo := openReminderRepoForTest(t)
	owner := createUserForReminderTest(t, repo, "reminder-owner@example.com")
	other := createUserForReminderTest(t, repo, "reminder-other@example.com")

	if err := repo.UpdateReminderLeadDays(context.Background(), other.ID, 2); err != nil {
		t.Fatalf("seed other owner reminder lead days: %v", err)
	}

	if err := repo.UpdateReminderLeadDays(context.Background(), owner.ID, 9); err != nil {
		t.Fatalf("UpdateReminderLeadDays owner: %v", err)
	}

	if got := reloadReminderLeadDays(t, repo, owner.ID); got != 9 {
		t.Fatalf("expected owner reminder_lead_days 9, got %d", got)
	}
	if got := reloadReminderLeadDays(t, repo, other.ID); got != 2 {
		t.Fatalf("expected other owner reminder_lead_days untouched at 2, got %d", got)
	}
}

func TestUpdateReminderLeadDaysUnknownUserIsNoop(t *testing.T) {
	repo := openReminderRepoForTest(t)

	if err := repo.UpdateReminderLeadDays(context.Background(), 99999, 7); err != nil {
		t.Fatalf("expected no error updating nonexistent user, got %v", err)
	}
}
