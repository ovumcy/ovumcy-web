package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func mapDayRangeError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrDayRangeFromInvalid):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid from date")
	case errors.Is(err, services.ErrDayRangeToInvalid):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid to date")
	case errors.Is(err, services.ErrDayRangeInvalid):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid range")
	default:
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid range")
	}
}

func mapDayUpsertError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrManualCycleStartDateInvalid):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid cycle start day")
	case errors.Is(err, services.ErrManualCycleStartReplaceRequired):
		return globalErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "cycle start replace required")
	case errors.Is(err, services.ErrManualCycleStartConfirmationNeeded):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "cycle start confirmation required")
	case errors.Is(err, services.ErrInvalidDayFlow):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid flow value")
	case errors.Is(err, services.ErrInvalidDayMood):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid mood value")
	case errors.Is(err, services.ErrInvalidDaySexActivity):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid sex activity value")
	case errors.Is(err, services.ErrInvalidDayBBT):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid bbt value")
	case errors.Is(err, services.ErrInvalidDayCervicalMucus):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid cervical mucus value")
	case errors.Is(err, services.ErrInvalidDayPregnancyTest):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid pregnancy test value")
	case errors.Is(err, services.ErrInvalidDayCycleFactors):
		return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid cycle factor values")
	case errors.Is(err, services.ErrDayAutoFillLoadFailed),
		errors.Is(err, services.ErrDayAutoFillCheckFailed),
		errors.Is(err, services.ErrDayEntryLoadFailed):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day")
	case errors.Is(err, services.ErrDayEntryCreateFailed):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create day")
	case errors.Is(err, services.ErrDayAutoFillApplyFailed),
		errors.Is(err, services.ErrDayEntryUpdateFailed):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update day")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update day")
	}
}

func mapDayDeleteError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrDeleteDayFailed):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to delete day")
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
