package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func mapRecoveryCodeRegenerationError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrRecoveryCodeGenerate):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create recovery code")
	case errors.Is(err, services.ErrRecoveryCodeUpdate):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update recovery code")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update recovery code")
	}
}

func settingsLoadErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load settings")
}
