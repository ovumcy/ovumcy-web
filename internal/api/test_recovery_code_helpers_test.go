package api

import (
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
	"gorm.io/gorm"
)

func mustSetRecoveryCodeForUser(t *testing.T, database *gorm.DB, userID uint) string {
	t.Helper()

	recoveryCode, recoveryHash, err := services.GenerateRecoveryCodeHash()
	if err != nil {
		t.Fatalf("generate recovery code: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", userID).Update("recovery_code_hash", recoveryHash).Error; err != nil {
		t.Fatalf("update recovery hash: %v", err)
	}
	return recoveryCode
}
