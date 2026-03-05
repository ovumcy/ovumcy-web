package api

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestMapExportRangeError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "invalid from",
			err:  services.ErrExportFromDateInvalid,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid from date"),
		},
		{
			name: "invalid to",
			err:  services.ErrExportToDateInvalid,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid to date"),
		},
		{
			name: "invalid range",
			err:  services.ErrExportRangeInvalid,
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
			if got := mapExportRangeError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}
