package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// mapLocalPasswordSetupReauthError maps failures of the OIDC step-up exchange
// that gates local-password enrollment and, through the same helper, the two
// erasure step-ups. The freshness verdicts and the identity mismatch keep their
// own specs so the owner learns what to do next — and the two freshness
// verdicts are separate specs because their "next" differs: sign in again, or
// stop and fix the provider. Everything else collapses into the generic SSO
// failure so provider state never leaks through error granularity.
func mapLocalPasswordSetupReauthError(err error) APIErrorSpec {
	switch {
	// Before the stale arm, always: ErrOIDCReauthAuthTimeMissing wraps
	// ErrOIDCReauthStale, so the coarse match below would swallow it and tell
	// the owner to retry something that cannot succeed. Pinned by
	// TestEveryReauthStaleMatchIsPrecededByTheMissingAuthTimeMatch.
	case errors.Is(err, services.ErrOIDCReauthAuthTimeMissing):
		return settingsOIDCReauthAuthTimeMissingErrorSpec()
	case errors.Is(err, services.ErrOIDCReauthStale):
		return settingsOIDCReauthStaleErrorSpec()
	case errors.Is(err, services.ErrOIDCReauthIdentityMismatch):
		return settingsOIDCReauthMismatchErrorSpec()
	case errors.Is(err, services.ErrOIDCDisabled), errors.Is(err, services.ErrOIDCUnavailable):
		return authOIDCUnavailableErrorSpec()
	default:
		return authOIDCAuthenticationFailedErrorSpec()
	}
}

// passwordChangedSignInAgainErrorSpec answers a ChangePassword whose write
// (the new hash and the AuthSessionVersion bump) already committed but whose
// session could not be re-issued afterward. Same reasoning as
// settingsDataClearedSignInAgainErrorSpec: the password already changed, so
// "failed to create session" would be false, and 401 says the right next step
// is signing in again — with the NEW password — since the caller has already
// cleared the cookie. Built with settingsFormErrorSpec, matching every other
// spec mapSettingsPasswordChangeError below returns.
func passwordChangedSignInAgainErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "password changed sign in again")
}

// WEB-54: ErrSettingsInvalidCurrentPassword and ErrSettingsLocalPasswordNotSet
// answer IDENTICALLY below — same 401, same
// SettingsPasswordChangeKeyInvalidCurrent key, same settings_form target — so
// a wrong current password and "this account has no local password yet" are
// indistinguishable to the caller; ValidatePasswordChange already equalizes
// their bcrypt cost (SEC-L3, WEB-13). The distinction survives only in the
// server security log, via settingsReauthCauseField. In practice
// ChangePassword's own !user.LocalAuthEnabled gate (handlers_settings_password.go)
// answers an OIDC-only account with the separate, documented "oidc reauth
// required" refusal before this mapper ever runs, so the merged arm here is
// defense-in-depth for an account whose password hash is empty despite that
// gate having passed — ValidatePasswordChange raises
// ErrSettingsLocalPasswordNotSet on an empty PasswordHash as well as on a
// cleared LocalAuthEnabled flag (WEB-112), so only the empty-hash state can
// reach this mapper past the gate — but it must still fail closed to the SAME
// answer, never to the old distinguishable one. Regression:
// TestSettingsReauthMergesNoLocalPasswordIntoInvalidPassword.
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
	case errors.Is(err, services.ErrSettingsInvalidCurrentPassword), errors.Is(err, services.ErrSettingsLocalPasswordNotSet):
		return settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, services.SettingsPasswordChangeKeyInvalidCurrent)
	case errors.Is(err, services.ErrSettingsNewPasswordMustDiffer):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyMustDiffer)
	case errors.Is(err, services.ErrSettingsPasswordTooLong):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyPasswordTooLong)
	case errors.Is(err, services.ErrSettingsWeakPassword):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, services.SettingsPasswordChangeKeyWeakPassword)
	case errors.Is(err, services.ErrSettingsPasswordHashFailed), errors.Is(err, services.ErrSettingsRecoveryCodeGenerateFailed):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to secure password")
	// services.ErrSettingsPasswordUpdateFailed and an unrecognized error share
	// the default: past the refusals above the change can only have failed on
	// the way to storage, and a finer message would describe the account state
	// the write left behind.
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update password")
	}
}
