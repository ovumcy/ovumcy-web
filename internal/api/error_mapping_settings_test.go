package api

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestSettingsGeneralErrorSpecs(t *testing.T) {
	testCases := []struct {
		name string
		got  APIErrorSpec
		want APIErrorSpec
	}{
		{
			name: "validation",
			got:  settingsValidationErrorSpec("invalid settings input"),
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid settings input"),
		},
		{
			name: "missing password",
			got:  settingsMissingPasswordErrorSpec(),
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid password"),
		},
		{
			name: "invalid password",
			got:  settingsInvalidPasswordErrorSpec(),
			want: settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid password"),
		},
		{
			name: "cycle update",
			got:  settingsCycleUpdateErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update cycle settings"),
		},
		{
			name: "clear data",
			got:  settingsClearDataErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to clear data"),
		},
		{
			name: "validate password",
			got:  settingsValidatePasswordErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to validate password"),
		},
		{
			name: "delete account",
			got:  settingsDeleteAccountErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to delete account"),
		},
		{
			name: "profile update",
			got:  settingsProfileUpdateErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update profile"),
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

func TestMapSettingsProfileNormalizeError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "display name too long",
			err:  services.ErrSettingsDisplayNameTooLong,
			want: settingsValidationErrorSpec("display name too long"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: settingsValidationErrorSpec("invalid profile input"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapSettingsProfileNormalizeError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapSettingsDeleteAccountPasswordError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "missing password",
			err:  services.ErrSettingsPasswordMissing,
			want: settingsMissingPasswordErrorSpec(),
		},
		{
			name: "invalid password",
			err:  services.ErrSettingsPasswordInvalid,
			want: settingsInvalidPasswordErrorSpec(),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: settingsValidatePasswordErrorSpec(),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapSettingsDeleteAccountPasswordError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}
