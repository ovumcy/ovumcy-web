package api

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestMapSymptomCreateError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "name required",
			err:  services.ErrSymptomNameRequired,
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name is required"),
		},
		{
			name: "name too long",
			err:  services.ErrSymptomNameTooLong,
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name is too long"),
		},
		{
			name: "name invalid characters",
			err:  services.ErrSymptomNameInvalidCharacters,
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name contains invalid characters"),
		},
		{
			name: "invalid color",
			err:  services.ErrInvalidSymptomColor,
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid symptom color"),
		},
		{
			name: "duplicate name",
			err:  services.ErrSymptomNameAlreadyExists,
			want: settingsFormErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "symptom name already exists"),
		},
		{
			name: "create failed",
			err:  services.ErrCreateSymptomFailed,
			want: settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create symptom"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create symptom"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapSymptomCreateError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapSymptomUpdateArchiveAndRestoreErrors(t *testing.T) {
	testCases := []struct {
		name string
		got  APIErrorSpec
		want APIErrorSpec
	}{
		{
			name: "update builtin forbidden",
			got:  mapSymptomUpdateError(services.ErrBuiltinSymptomEditForbidden),
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "built-in symptom cannot be edited"),
		},
		{
			name: "update name too long",
			got:  mapSymptomUpdateError(services.ErrSymptomNameTooLong),
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name is too long"),
		},
		{
			name: "update name required",
			got:  mapSymptomUpdateError(services.ErrSymptomNameRequired),
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name is required"),
		},
		{
			name: "update name invalid characters",
			got:  mapSymptomUpdateError(services.ErrSymptomNameInvalidCharacters),
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "symptom name contains invalid characters"),
		},
		{
			name: "update invalid color",
			got:  mapSymptomUpdateError(services.ErrInvalidSymptomColor),
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid symptom color"),
		},
		{
			name: "update failed",
			got:  mapSymptomUpdateError(services.ErrUpdateSymptomFailed),
			want: settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update symptom"),
		},
		{
			name: "archive builtin forbidden",
			got:  mapSymptomArchiveError(services.ErrBuiltinSymptomHideForbidden),
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "built-in symptom cannot be hidden"),
		},
		{
			name: "restore builtin forbidden",
			got:  mapSymptomRestoreError(services.ErrBuiltinSymptomShowForbidden),
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "built-in symptom cannot be restored"),
		},
		{
			name: "restore duplicate name",
			got:  mapSymptomRestoreError(services.ErrSymptomNameAlreadyExists),
			want: settingsFormErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "symptom name already exists"),
		},
		{
			name: "archive failed",
			got:  mapSymptomArchiveError(services.ErrArchiveSymptomFailed),
			want: settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to hide symptom"),
		},
		{
			name: "restore failed",
			got:  mapSymptomRestoreError(services.ErrRestoreSymptomFailed),
			want: settingsFormErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to restore symptom"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", testCase.got, testCase.want)
			}
		})
	}
}
