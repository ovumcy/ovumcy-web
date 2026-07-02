package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func mapSettingsPasswordChangeError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrSettingsPasswordChangeInvalidInput):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyInvalidInput)
	case errors.Is(err, services.ErrSettingsPasswordMismatch):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyPasswordMismatch)
	case errors.Is(err, services.ErrSettingsInvalidCurrentPassword):
		return settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, services.SettingsPasswordChangeKeyInvalidCurrent)
	case errors.Is(err, services.ErrSettingsLocalPasswordNotSet):
		return settingsLocalPasswordRequiredErrorSpec()
	case errors.Is(err, services.ErrSettingsNewPasswordMustDiffer):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyMustDiffer)
	case errors.Is(err, services.ErrSettingsWeakPassword):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyWeakPassword)
	case errors.Is(err, services.ErrSettingsPasswordHashFailed), errors.Is(err, services.ErrSettingsRecoveryCodeGenerateFailed):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to secure password")
	case errors.Is(err, services.ErrSettingsPasswordUpdateFailed):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update password")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update password")
	}
}
