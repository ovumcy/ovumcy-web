package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// mapRecoveryCodeRegenerationError narrows only the generation failure; the
// persistence failure and an unrecognized error share the default, since after
// generation the regeneration can only have failed on the way to storage.
func mapRecoveryCodeRegenerationError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrRecoveryCodeGenerate):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create recovery code")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update recovery code")
	}
}

func settingsLoadErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load settings")
}
