package api

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestMapDayRangeError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "invalid from",
			err:  services.ErrDayRangeFromInvalid,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid from date"),
		},
		{
			name: "invalid to",
			err:  services.ErrDayRangeToInvalid,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid to date"),
		},
		{
			name: "invalid range",
			err:  services.ErrDayRangeInvalid,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid range"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid range"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapDayRangeError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapDayUpsertError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "invalid flow",
			err:  services.ErrInvalidDayFlow,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid flow value"),
		},
		{
			name: "load failed",
			err:  services.ErrDayEntryLoadFailed,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day"),
		},
		{
			name: "create failed",
			err:  services.ErrDayEntryCreateFailed,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create day"),
		},
		{
			name: "update failed",
			err:  services.ErrDayEntryUpdateFailed,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update day"),
		},
		{
			name: "sync failed",
			err:  services.ErrSyncLastPeriodFailed,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to sync last period start"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update day"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapDayUpsertError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapDayDeleteError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "delete failed",
			err:  services.ErrDeleteDayFailed,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to delete day"),
		},
		{
			name: "sync failed",
			err:  services.ErrSyncLastPeriodFailed,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to sync last period start"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to delete day"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapDayDeleteError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}
