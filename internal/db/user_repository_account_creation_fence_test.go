package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func accountCreationFenceUser(email string) models.User {
	return models.User{
		Email:               email,
		PasswordHash:        "hash",
		RecoveryCodeHash:    "recovery",
		Role:                models.RoleOwner,
		LocalAuthEnabled:    true,
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
		CreatedAt:           time.Now().UTC(),
	}
}

func countUsersWithEmail(t *testing.T, fixture signOutFenceFixture, email string) int64 {
	t.Helper()
	var count int64
	if err := fixture.repositories.Users.database.Model(&models.User{}).Where("email = ?", email).Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	return count
}

// TestAnAccountCreationOverAPersistentlyFailingFenceFileIsRefused is WEB-538's
// refusal arm, on both dialects and for both inserts of a users row: a signup
// or provisioning that meets a fence file failing every retry is refused as an
// ordinary failure, with no row and neither fence half moved, and goes through
// once the fence volume writes again, moving both halves to one fresh token.
func TestAnAccountCreationOverAPersistentlyFailingFenceFileIsRefused(t *testing.T) {
	creators := map[string]func(repo *UserRepository, user *models.User) error{
		"Create": func(repo *UserRepository, user *models.User) error {
			return repo.Create(context.Background(), user)
		},
		"CreateUserWithSymptoms": func(repo *UserRepository, user *models.User) error {
			return repo.CreateUserWithSymptoms(context.Background(), user, []models.SymptomType{{Name: "Cramps", Icon: "x"}})
		},
	}
	for dialect, open := range signOutFenceDialects() {
		for creator, create := range creators {
			t.Run(dialect+"/"+creator, func(t *testing.T) {
				fixture := newSignOutFenceFixture(t, open(t))
				fileBefore, storedBefore := fixture.halves(t)
				email := "refused-creation@example.com"

				fixture.file.failing = true
				user := accountCreationFenceUser(email)
				err := create(fixture.repositories.Users, &user)
				if !errors.Is(err, services.ErrCalendarFeedFenceWriteFailed) {
					t.Fatalf("a creation over a persistently failing fence file must be refused by the fence, got %v", err)
				}
				if got := countUsersWithEmail(t, fixture, email); got != 0 {
					t.Fatalf("a refused creation must write no row, found %d", got)
				}
				if fileAfter, storedAfter := fixture.halves(t); fileAfter != fileBefore || storedAfter != storedBefore {
					t.Fatalf("a refused creation must write neither half: file moved %t, database moved %t", fileAfter != fileBefore, storedAfter != storedBefore)
				}

				fixture.file.failing = false
				user = accountCreationFenceUser(email)
				if err := create(fixture.repositories.Users, &user); err != nil {
					t.Fatalf("the retried creation: %v", err)
				}
				if got := countUsersWithEmail(t, fixture, email); got != 1 {
					t.Fatalf("the retried creation must write its row, found %d", got)
				}
				if fileAfter, storedAfter := fixture.halves(t); fileAfter != storedAfter || storedAfter == storedBefore {
					t.Fatal("a creation must move both fence halves to one fresh token")
				}
			})
		}
	}
}
