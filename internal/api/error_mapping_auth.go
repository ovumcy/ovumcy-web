package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func authValidationErrorSpec(key string) APIErrorSpec {
	return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, key)
}

func authInvalidInputErrorSpec() APIErrorSpec {
	return authValidationErrorSpec("invalid input")
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
	switch services.ClassifyAuthRegisterError(err) {
	case services.AuthRegisterErrorRegistrationDisabled:
		return authFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "registration disabled")
	case services.AuthRegisterErrorPasswordMismatch:
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "password mismatch")
	case services.AuthRegisterErrorWeakPassword:
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "weak password")
	case services.AuthRegisterErrorEmailExists:
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input")
	case services.AuthRegisterErrorInvalidInput:
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input")
	case services.AuthRegisterErrorSeedSymptoms:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to seed symptoms")
	case services.AuthRegisterErrorFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create account")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create account")
	}
}

func mapAuthLoginError(err error) APIErrorSpec {
	switch services.ClassifyAuthLoginError(err) {
	case services.AuthLoginErrorRateLimited:
		return authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many login attempts")
	case services.AuthLoginErrorUnsupportedRole:
		return authWebSignInUnavailableErrorSpec()
	case services.AuthLoginErrorResetTokenIssue:
		return authResetTokenCreateErrorSpec()
	case services.AuthLoginErrorInvalidCredentials:
		return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials")
	default:
		return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials")
	}
}

func mapAuthOIDCError(err error) APIErrorSpec {
	switch services.ClassifyOIDCAuthError(err) {
	case services.OIDCAuthErrorDisabled, services.OIDCAuthErrorUnavailable:
		return authOIDCUnavailableErrorSpec()
	case services.OIDCAuthErrorCallbackInvalid, services.OIDCAuthErrorAuthenticationFailed:
		return authOIDCAuthenticationFailedErrorSpec()
	case services.OIDCAuthErrorAccountUnavailable:
		return authOIDCAccountUnavailableErrorSpec()
	case services.OIDCAuthErrorIdentityResolveFailed, services.OIDCAuthErrorLinkFailed, services.OIDCAuthErrorProvisionFailed:
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
	switch services.ClassifyPasswordRecoveryStartError(err) {
	case services.PasswordRecoveryStartErrorRateLimited:
		return authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many recovery attempts")
	case services.PasswordRecoveryStartErrorInvalidInput:
		return authInvalidInputErrorSpec()
	case services.PasswordRecoveryStartErrorInvalidCode:
		return authValidationErrorSpec("invalid recovery code")
	default:
		return authResetTokenCreateErrorSpec()
	}
}

func mapPasswordResetCompleteError(err error) APIErrorSpec {
	switch services.ClassifyPasswordResetCompleteError(err) {
	case services.PasswordResetCompleteErrorPasswordMismatch:
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "password mismatch")
	case services.PasswordResetCompleteErrorWeakPassword:
		return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "weak password")
	case services.PasswordResetCompleteErrorInvalidInput:
		return authInvalidInputErrorSpec()
	case services.PasswordResetCompleteErrorInvalidToken:
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

func authResetTokenCreateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create reset token")
}

func authRecoveryCodePersistErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to persist recovery code")
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
