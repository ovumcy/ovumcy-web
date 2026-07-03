package api

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
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
			name: "invalid cycle start day",
			err:  services.ErrManualCycleStartDateInvalid,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid cycle start day"),
		},
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
			name: "invalid cycle factors",
			err:  services.ErrInvalidDayCycleFactors,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid cycle factor values"),
		},
		{
			name: "invalid pregnancy test",
			err:  services.ErrInvalidDayPregnancyTest,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid pregnancy test value"),
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
			name: "cycle start replace required",
			err:  services.ErrManualCycleStartReplaceRequired,
			want: globalErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "cycle start replace required"),
		},
		{
			name: "cycle start confirmation required",
			err:  services.ErrManualCycleStartConfirmationNeeded,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "cycle start confirmation required"),
		},
		{
			name: "invalid mood",
			err:  services.ErrInvalidDayMood,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid mood value"),
		},
		{
			name: "invalid sex activity",
			err:  services.ErrInvalidDaySexActivity,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid sex activity value"),
		},
		{
			name: "invalid bbt",
			err:  services.ErrInvalidDayBBT,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid bbt value"),
		},
		{
			name: "invalid cervical mucus",
			err:  services.ErrInvalidDayCervicalMucus,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid cervical mucus value"),
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
