package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// mapSymptomNameValidationError maps the error cases common to create and
// update; returns (spec, true) on match, (zero, false) otherwise so callers
// can fall through to their own operation-specific cases.
func mapSymptomNameValidationError(err error) (APIErrorSpec, bool) {
	switch {
	case errors.Is(err, services.ErrSymptomNameRequired):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name is required"), true
	case errors.Is(err, services.ErrSymptomNameTooLong):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name is too long"), true
	case errors.Is(err, services.ErrSymptomNameInvalidCharacters):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name contains invalid characters"), true
	case errors.Is(err, services.ErrInvalidSymptomColor):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid symptom color"), true
	case errors.Is(err, services.ErrSymptomNameAlreadyExists):
		return settingsFormErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "symptom name already exists"), true
	default:
		return APIErrorSpec{}, false
	}
}

// mapSymptomCreateError has one non-validation outcome by construction:
// services.ErrCreateSymptomFailed is the only failure the create path reports
// past name validation, and an unrecognized error is answered the same way.
func mapSymptomCreateError(err error) APIErrorSpec {
	if spec, ok := mapSymptomNameValidationError(err); ok {
		return spec
	}
	return settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create symptom")
}

func mapSymptomUpdateError(err error) APIErrorSpec {
	if spec, ok := mapSymptomNameValidationError(err); ok {
		return spec
	}
	switch {
	case errors.Is(err, services.ErrSymptomNotFound):
		return settingsFormErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "symptom not found")
	case errors.Is(err, services.ErrBuiltinSymptomEditForbidden):
		return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "built-in symptom cannot be edited")
	// services.ErrUpdateSymptomFailed and an unrecognized error share the
	// default: past the refusals above, the update can only have failed on the
	// way to storage.
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
	// services.ErrArchiveSymptomFailed and an unrecognized error share the
	// default: past the refusals above, the archive can only have failed on the
	// way to storage.
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
	// services.ErrRestoreSymptomFailed and an unrecognized error share the
	// default: past the refusals above, the restore can only have failed on the
	// way to storage.
	default:
		return settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to restore symptom")
	}
}

func symptomsFetchErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to fetch symptoms")
}
