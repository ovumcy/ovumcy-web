package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func settingsValidationErrorSpec(key string) APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, key)
}

func settingsInvalidInputErrorSpec() APIErrorSpec {
	return settingsValidationErrorSpec("invalid settings input")
}

func settingsMissingPasswordErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid password")
}

func settingsInvalidPasswordErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid password")
}

func settingsCycleUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update cycle settings")
}

func settingsClearDataErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to clear data")
}

func settingsValidatePasswordErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to validate password")
}

func settingsDeleteAccountErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to delete account")
}

func settingsProfileUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update profile")
}

func mapSettingsProfileNormalizeError(err error) APIErrorSpec {
	switch services.ClassifySettingsProfileError(err) {
	case services.SettingsProfileErrorDisplayNameTooLong:
		return settingsValidationErrorSpec("display name too long")
	default:
		return settingsValidationErrorSpec("invalid profile input")
	}
}

func mapSettingsDeleteAccountPasswordError(err error) APIErrorSpec {
	switch services.ClassifySettingsDeleteAccountPasswordError(err) {
	case services.SettingsDeleteAccountPasswordErrorMissing:
		return settingsMissingPasswordErrorSpec()
	case services.SettingsDeleteAccountPasswordErrorInvalid:
		return settingsInvalidPasswordErrorSpec()
	default:
		return settingsValidatePasswordErrorSpec()
	}
}
