package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// mapExportRangeError maps ExportService range-parsing domain errors to
// transport specs. All cases (including unknown) are the same 400/validation
// category; only the message narrows to the specific bad field.
func mapExportRangeError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrExportFromDateInvalid):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid from date")
	case errors.Is(err, services.ErrExportToDateInvalid):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid to date")
	case errors.Is(err, services.ErrExportRangeInvalid):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid range")
	default:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid range")
	}
}

func exportFetchLogsErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to fetch logs")
}

func exportBuildErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to build export")
}
