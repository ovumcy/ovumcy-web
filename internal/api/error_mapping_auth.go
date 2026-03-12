package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func authValidationErrorSpec(key string) APIErrorSpec {
	return authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, key)
}

func authInvalidInputErrorSpec() APIErrorSpec {
	return authValidationErrorSpec("invalid input")
}

func invalidResetTokenErrorSpec() APIErrorSpec {
	return authValidationErrorSpec("invalid reset token")
}

func passwordChangeRequiredErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "password change required")
}

func mapAuthRegisterError(err error) APIErrorSpec {
	switch services.ClassifyAuthRegisterError(err) {
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
	case services.AuthLoginErrorResetTokenIssue:
		return authResetTokenCreateErrorSpec()
	case services.AuthLoginErrorInvalidCredentials:
		return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials")
	default:
		return authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials")
	}
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
