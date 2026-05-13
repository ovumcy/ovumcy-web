package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
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

func settingsLocalPasswordRequiredErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "local password required")
}

// settingsOIDCReauthRequiredErrorSpec is returned when an OIDC-only user
// attempts to enable a local password via the legacy ChangePassword endpoint
// instead of going through StartLocalPasswordSetupReauth. It signals the UI
// to redirect into the step-up flow.
func settingsOIDCReauthRequiredErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "oidc reauth required")
}

func settingsOIDCReauthStaleErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "oidc reauth stale")
}

func settingsOIDCReauthMismatchErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "oidc reauth identity mismatch")
}

func settingsCycleUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update cycle settings")
}

func settingsTrackingUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update tracking settings")
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
	case services.SettingsProfileErrorDisplayNameInvalidCharacters:
		return settingsValidationErrorSpec("display name contains invalid characters")
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
	case services.SettingsDeleteAccountPasswordErrorLocalPasswordNotSet:
		return settingsLocalPasswordRequiredErrorSpec()
	default:
		return settingsValidatePasswordErrorSpec()
	}
}
