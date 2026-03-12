package api

import (
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
	"gorm.io/gorm"
)

func assertClearDataPostconditions(t *testing.T, database *gorm.DB, user models.User) {
	t.Helper()

	var logsCount int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ?", user.ID).Count(&logsCount).Error; err != nil {
		t.Fatalf("count logs: %v", err)
	}
	if logsCount != 0 {
		t.Fatalf("expected logs to be deleted, got %d", logsCount)
	}

	var customCount int64
	if err := database.Model(&models.SymptomType{}).Where("user_id = ? AND is_builtin = ?", user.ID, false).Count(&customCount).Error; err != nil {
		t.Fatalf("count custom symptoms: %v", err)
	}
	if customCount != 0 {
		t.Fatalf("expected custom symptoms to be deleted, got %d", customCount)
	}

	var builtinCount int64
	if err := database.Model(&models.SymptomType{}).Where("user_id = ? AND is_builtin = ?", user.ID, true).Count(&builtinCount).Error; err != nil {
		t.Fatalf("count builtin symptoms: %v", err)
	}
	if builtinCount == 0 {
		t.Fatalf("expected builtin symptoms to be preserved")
	}

	var preservedBuiltinCount int64
	if err := database.Model(&models.SymptomType{}).Where("user_id = ? AND is_builtin = ? AND name = ?", user.ID, true, "Builtin").Count(&preservedBuiltinCount).Error; err != nil {
		t.Fatalf("count preserved seeded builtin symptom: %v", err)
	}
	if preservedBuiltinCount != 1 {
		t.Fatalf("expected seeded builtin symptom to be preserved, got %d", preservedBuiltinCount)
	}

	updatedUser := models.User{}
	if err := database.First(&updatedUser, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if updatedUser.CycleLength != 28 {
		t.Fatalf("expected cycle length reset to 28, got %d", updatedUser.CycleLength)
	}
	if updatedUser.PeriodLength != 5 {
		t.Fatalf("expected period length reset to 5, got %d", updatedUser.PeriodLength)
	}
	if !updatedUser.AutoPeriodFill {
		t.Fatalf("expected auto period fill reset to true")
	}
	if updatedUser.LastPeriodStart != nil {
		t.Fatalf("expected last period start to be cleared, got %v", updatedUser.LastPeriodStart)
	}
}
