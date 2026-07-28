package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// mapLocalPasswordSetupReauthError maps failures of the OIDC step-up exchange
// that gates local-password enrollment. Stale and identity-mismatch outcomes
// keep their own specs so the owner learns to redo the step-up; everything
// else collapses into the generic SSO failure so provider state never leaks
// through error granularity.
func mapLocalPasswordSetupReauthError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrOIDCReauthStale):
		return settingsOIDCReauthStaleErrorSpec()
	case errors.Is(err, services.ErrOIDCReauthIdentityMismatch):
		return settingsOIDCReauthMismatchErrorSpec()
	case errors.Is(err, services.ErrOIDCCallbackInvalid):
		return authOIDCAuthenticationFailedErrorSpec()
	case errors.Is(err, services.ErrOIDCAuthenticationFailed):
		return authOIDCAuthenticationFailedErrorSpec()
	case errors.Is(err, services.ErrOIDCDisabled), errors.Is(err, services.ErrOIDCUnavailable):
		return authOIDCUnavailableErrorSpec()
	default:
		return authOIDCAuthenticationFailedErrorSpec()
	}
}

func mapSettingsPasswordChangeError(err error) APIErrorSpec {
	switch {
	// Checked first: an exhausted re-auth budget is refused before the current
	// password is compared, so it must map to 429 rather than "invalid current
	// password" — otherwise the response itself would leak that the budget, not
	// the credential, was the blocker.
	case errors.Is(err, services.ErrSettingsReauthRateLimited):
		return settingsRateLimitErrorSpec()
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
