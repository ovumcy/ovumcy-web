package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func mapSymptomCreateError(err error) APIErrorSpec {
	switch services.ClassifySymptomCreateError(err) {
	case services.SymptomCreateErrorInvalidName:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid symptom name")
	case services.SymptomCreateErrorInvalidColor:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid symptom color")
	case services.SymptomCreateErrorFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create symptom")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create symptom")
	}
}

func mapSymptomDeleteError(err error) APIErrorSpec {
	switch services.ClassifySymptomDeleteError(err) {
	case services.SymptomDeleteErrorNotFound:
		return globalErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "symptom not found")
	case services.SymptomDeleteErrorBuiltinForbidden:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "built-in symptom cannot be deleted")
	case services.SymptomDeleteErrorDeleteFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to delete symptom")
	case services.SymptomDeleteErrorCleanLogsFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to clean symptom logs")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to delete symptom")
	}
}

func symptomsFetchErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to fetch symptoms")
}
