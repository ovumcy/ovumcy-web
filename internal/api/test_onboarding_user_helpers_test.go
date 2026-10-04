package api

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func createOnboardingTestUser(t *testing.T, database *gorm.DB, email string, password string, onboardingCompleted bool) models.User {
	t.Helper()
	return createOnboardingTestUserAt(t, database, email, password, onboardingCompleted, time.Now().UTC())
}

// createOnboardingTestUserAt dates the account explicitly. A test that pins the
// handler's clock must date the account against that clock: the calendar's
// look-back floor follows the account's age, so a wall-clock CreatedAt drifts
// the floor past the pinned months as real time advances.
func createOnboardingTestUserAt(t *testing.T, database *gorm.DB, email string, password string, onboardingCompleted bool, createdAt time.Time) models.User {
	t.Helper()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user := models.User{
		Email:               strings.ToLower(strings.TrimSpace(email)),
		PasswordHash:        string(passwordHash),
		LocalAuthEnabled:    true,
		Role:                models.RoleOwner,
		OnboardingCompleted: onboardingCompleted,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           createdAt,
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}
