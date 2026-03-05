package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func mapExportRangeError(err error) APIErrorSpec {
	switch services.ClassifyExportRangeError(err) {
	case services.ExportRangeErrorFromInvalid:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid from date")
	case services.ExportRangeErrorToInvalid:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid to date")
	case services.ExportRangeErrorInvalid:
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
