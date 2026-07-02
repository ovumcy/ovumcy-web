package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func mapSymptomCreateError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrSymptomNameRequired):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name is required")
	case errors.Is(err, services.ErrSymptomNameTooLong):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name is too long")
	case errors.Is(err, services.ErrSymptomNameInvalidCharacters):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name contains invalid characters")
	case errors.Is(err, services.ErrInvalidSymptomColor):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid symptom color")
	case errors.Is(err, services.ErrSymptomNameAlreadyExists):
		return settingsFormErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "symptom name already exists")
	case errors.Is(err, services.ErrCreateSymptomFailed):
		return settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create symptom")
	default:
		return settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create symptom")
	}
}

func mapSymptomUpdateError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrSymptomNotFound):
		return settingsFormErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "symptom not found")
	case errors.Is(err, services.ErrSymptomNameRequired):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name is required")
	case errors.Is(err, services.ErrSymptomNameTooLong):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name is too long")
	case errors.Is(err, services.ErrSymptomNameInvalidCharacters):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name contains invalid characters")
	case errors.Is(err, services.ErrInvalidSymptomColor):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid symptom color")
	case errors.Is(err, services.ErrSymptomNameAlreadyExists):
		return settingsFormErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "symptom name already exists")
	case errors.Is(err, services.ErrBuiltinSymptomEditForbidden):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "built-in symptom cannot be edited")
	case errors.Is(err, services.ErrUpdateSymptomFailed):
		return settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update symptom")
	default:
		return settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update symptom")
	}
}

func mapSymptomArchiveError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrSymptomNotFound):
		return settingsFormErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "symptom not found")
	case errors.Is(err, services.ErrBuiltinSymptomHideForbidden):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "built-in symptom cannot be hidden")
	case errors.Is(err, services.ErrArchiveSymptomFailed):
		return settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to hide symptom")
	default:
		return settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to hide symptom")
	}
}

func mapSymptomRestoreError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrSymptomNotFound):
		return settingsFormErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "symptom not found")
	case errors.Is(err, services.ErrBuiltinSymptomShowForbidden):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "built-in symptom cannot be restored")
	case errors.Is(err, services.ErrSymptomNameAlreadyExists):
		return settingsFormErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "symptom name already exists")
	case errors.Is(err, services.ErrRestoreSymptomFailed):
		return settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to restore symptom")
	default:
		return settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to restore symptom")
	}
}

func symptomsFetchErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to fetch symptoms")
}
