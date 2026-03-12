package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"gorm.io/gorm"
)

type clearDataScenario struct {
	app        *fiber.App
	database   *gorm.DB
	user       models.User
	authCookie string
	csrfCookie *http.Cookie
	csrfToken  string
}

func setupClearDataScenario(t *testing.T) clearDataScenario {
	t.Helper()

	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "clear-data@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	lastPeriodStart := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      41,
		"period_length":     8,
		"auto_period_fill":  false,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user baseline settings: %v", err)
	}

	builtinSymptom := models.SymptomType{
		UserID:    user.ID,
		Name:      "Builtin",
		Icon:      "A",
		Color:     "#111111",
		IsBuiltin: true,
	}
	customSymptom := models.SymptomType{
		UserID:    user.ID,
		Name:      "Custom",
		Icon:      "B",
		Color:     "#222222",
		IsBuiltin: false,
	}
	if err := database.Create(&builtinSymptom).Error; err != nil {
		t.Fatalf("create builtin symptom: %v", err)
	}
	if err := database.Create(&customSymptom).Error; err != nil {
		t.Fatalf("create custom symptom: %v", err)
	}

	logEntry := models.DailyLog{
		UserID:     user.ID,
		Date:       time.Date(2026, time.February, 12, 0, 0, 0, 0, time.UTC),
		IsPeriod:   true,
		Flow:       models.FlowMedium,
		SymptomIDs: []uint{customSymptom.ID},
		Notes:      "test",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create log entry: %v", err)
	}

	return clearDataScenario{
		app:        app,
		database:   database,
		user:       user,
		authCookie: authCookie,
		csrfCookie: csrfCookie,
		csrfToken:  csrfToken,
	}
}
