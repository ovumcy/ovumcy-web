package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func mapRecoveryCodeRegenerationError(err error) APIErrorSpec {
	switch services.ClassifyRecoveryCodeRegenerationError(err) {
	case services.RecoveryCodeRegenerationErrorGenerateFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create recovery code")
	case services.RecoveryCodeRegenerationErrorUpdateFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update recovery code")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update recovery code")
	}
}

func settingsLoadErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load settings")
}
