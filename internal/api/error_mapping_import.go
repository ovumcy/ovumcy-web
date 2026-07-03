package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// mapImportError maps ImportService domain errors to transport specs. A restore
// either succeeds or fails as a whole, so every case is a global (not
// form-field-scoped) error. Stable keys are consumed by the settings JS to pick
// a localized message; they must not carry PII (the payload is the owner's own
// health data and is never echoed back).
func mapImportError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrImportMalformed):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid import file")
	case errors.Is(err, services.ErrImportTooLarge):
		return globalErrorSpec(fiber.StatusRequestEntityTooLarge, APIErrorCategoryValidation, "import file too large")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to import data")
	}
}
