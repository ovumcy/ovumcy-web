package api

import (
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func createOnboardingTestUser(t *testing.T, database *gorm.DB, email string, password string, onboardingCompleted bool) models.User {
	t.Helper()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user := models.User{
		Email:               strings.ToLower(strings.TrimSpace(email)),
		PasswordHash:        string(passwordHash),
		Role:                models.RoleOwner,
		OnboardingCompleted: onboardingCompleted,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}
