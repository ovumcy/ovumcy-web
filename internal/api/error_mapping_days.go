package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func mapDayRangeError(err error) APIErrorSpec {
	switch services.ClassifyDayRangeError(err) {
	case services.DayRangeErrorFromInvalid:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid from date")
	case services.DayRangeErrorToInvalid:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid to date")
	case services.DayRangeErrorInvalid:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid range")
	default:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid range")
	}
}

func mapDayUpsertError(err error) APIErrorSpec {
	switch services.ClassifyDayUpsertError(err) {
	case services.DayUpsertErrorInvalidFlow:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid flow value")
	case services.DayUpsertErrorLoadFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day")
	case services.DayUpsertErrorCreateFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create day")
	case services.DayUpsertErrorUpdateFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update day")
	case services.DayUpsertErrorSyncLastPeriodFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to sync last period start")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update day")
	}
}

func mapDayDeleteError(err error) APIErrorSpec {
	switch services.ClassifyDayDeleteError(err) {
	case services.DayDeleteErrorDeleteFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to delete day")
	case services.DayDeleteErrorSyncLastPeriodFailed:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to sync last period start")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to delete day")
	}
}

func invalidDateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid date")
}

func invalidPayloadErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid payload")
}

func invalidSymptomIDsErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid symptom ids")
}

func invalidSymptomIDErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid symptom id")
}

func dayLogsFetchErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to fetch logs")
}

func dayFetchErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to fetch day")
}
