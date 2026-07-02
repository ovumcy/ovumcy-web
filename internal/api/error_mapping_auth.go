package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func authValidationErrorSpec(key string) APIErrorSpec {
	return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, key)
}

func authInvalidInputErrorSpec() APIErrorSpec {
	return authValidationErrorSpec("invalid input")
}

func authConsentRequiredErrorSpec() APIErrorSpec {
	return authValidationErrorSpec("consent required")
}

func totpInvalidCodeErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "totp invalid code")
}

func totpSessionExpiredErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "totp session expired")
}

func totpInternalErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "totp internal error")
}

func totpRateLimitedErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "totp too many attempts")
}

func totpDisableRateLimitedErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "totp too many attempts")
}

func invalidResetTokenErrorSpec() APIErrorSpec {
	return authValidationErrorSpec("invalid reset token")
}

func passwordChangeRequiredErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "password change required")
}

func mapAuthRegisterError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrAuthRegistrationDisabled):
		return authFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "registration disabled")
	case errors.Is(err, services.ErrAuthPasswordMismatch):
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "password mismatch")
	case errors.Is(err, services.ErrAuthWeakPassword):
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "weak password")
	case errors.Is(err, services.ErrAuthEmailExists):
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input")
	case errors.Is(err, services.ErrAuthRegisterInvalid):
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input")
	case errors.Is(err, services.ErrRegistrationSeedSymptoms):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to seed symptoms")
	case errors.Is(err, services.ErrAuthRegisterFailed):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create account")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create account")
	}
}

func mapAuthLoginError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrAuthLoginRateLimited):
		return authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many login attempts")
	case errors.Is(err, services.ErrAuthUnsupportedRole):
		return authWebSignInUnavailableErrorSpec()
	case errors.Is(err, services.ErrLoginResetTokenIssue):
		return authResetTokenCreateErrorSpec()
	case errors.Is(err, services.ErrAuthInvalidCreds):
		return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials")
	default:
		return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials")
	}
}

func mapAuthOIDCError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrOIDCDisabled), errors.Is(err, services.ErrOIDCUnavailable):
		return authOIDCUnavailableErrorSpec()
	case errors.Is(err, services.ErrOIDCCallbackInvalid), errors.Is(err, services.ErrOIDCAuthenticationFailed):
		return authOIDCAuthenticationFailedErrorSpec()
	case errors.Is(err, services.ErrOIDCAccountUnavailable):
		return authOIDCAccountUnavailableErrorSpec()
	case errors.Is(err, services.ErrOIDCIdentityResolveFailed), errors.Is(err, services.ErrOIDCLinkFailed), errors.Is(err, services.ErrOIDCProvisionFailed):
		return authOIDCUnavailableErrorSpec()
	default:
		return authOIDCAuthenticationFailedErrorSpec()
	}
}

func authLocalSignInDisabledErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "local sign-in unavailable")
}

func authLocalRecoveryDisabledErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "local recovery unavailable")
}

func authWebSignInUnavailableErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "web sign-in unavailable")
}

func mapPasswordRecoveryStartError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrPasswordRecoveryRateLimited):
		return authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many recovery attempts")
	case errors.Is(err, services.ErrPasswordRecoveryInputInvalid):
		return authInvalidInputErrorSpec()
	case errors.Is(err, services.ErrPasswordRecoveryCodeInvalid):
		return authValidationErrorSpec("invalid recovery code")
	default:
		return authResetTokenCreateErrorSpec()
	}
}

func mapPasswordResetCompleteError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrAuthPasswordMismatch):
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "password mismatch")
	case errors.Is(err, services.ErrAuthWeakPassword):
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "weak password")
	case errors.Is(err, services.ErrAuthResetInvalid):
		return authInvalidInputErrorSpec()
	case errors.Is(err, services.ErrInvalidResetToken):
		return invalidResetTokenErrorSpec()
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to reset password")
	}
}

func authSessionCreateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create session")
}

func authSessionRevokeErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to revoke session")
}

func tooManyLogoutAttemptsErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many logout attempts")
}

func authResetTokenCreateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create reset token")
}

func authRecoveryCodePersistErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to persist recovery code")
}

func registerPickupCookieErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to issue register pickup")
}

func authOIDCUnavailableErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusServiceUnavailable, APIErrorCategoryInternal, "sso temporarily unavailable")
}

func authOIDCAuthenticationFailedErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "sso authentication failed")
}

func authOIDCAccountUnavailableErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "sso sign-in unavailable")
}

func authOIDCLinkConfirmExpiredErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "sso link confirmation expired")
}

func authOIDCLinkConfirmInvalidPasswordErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "sso link confirmation invalid password")
}

func authOIDCLinkConfirmRateLimitedErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many login attempts")
}

// mapOIDCLinkConfirmPasswordError maps failures of the link-confirm password
// verification (which runs through LoginService.Authenticate) onto the
// link-confirm error contract: rate-limited and reset-token-issue failures
// keep their own specs, everything else is the generic invalid-password
// response so account state never leaks through error granularity.
func mapOIDCLinkConfirmPasswordError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrAuthLoginRateLimited):
		return authOIDCLinkConfirmRateLimitedErrorSpec()
	case errors.Is(err, services.ErrLoginResetTokenIssue):
		return authResetTokenCreateErrorSpec()
	default:
		return authOIDCLinkConfirmInvalidPasswordErrorSpec()
	}
}

func authOIDCLinkConfirmUnavailableErrorSpec() APIErrorSpec {
	return authFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "sso link confirmation unavailable")
}
