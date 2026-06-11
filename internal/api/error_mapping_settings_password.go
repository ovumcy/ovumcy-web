package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func mapSettingsPasswordChangeError(err error) APIErrorSpec {
	switch services.ClassifySettingsPasswordChangeError(err) {
	case services.SettingsPasswordChangeErrorInvalidInput:
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyInvalidInput)
	case services.SettingsPasswordChangeErrorPasswordMismatch:
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyPasswordMismatch)
	case services.SettingsPasswordChangeErrorInvalidCurrentPassword:
		return settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, services.SettingsPasswordChangeKeyInvalidCurrent)
	case services.SettingsPasswordChangeErrorLocalPasswordNotSet:
		return settingsLocalPasswordRequiredErrorSpec()
	case services.SettingsPasswordChangeErrorNewPasswordMustDiffer:
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyMustDiffer)
	case services.SettingsPasswordChangeErrorWeakPassword:
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyWeakPassword)
	case services.SettingsPasswordChangeErrorHashFailed, services.SettingsPasswordChangeErrorRecoveryCodeFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to secure password")
	case services.SettingsPasswordChangeErrorUpdateFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update password")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update password")
	}
}
