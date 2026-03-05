package api

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestCommonErrorSpecs(t *testing.T) {
	testCases := []struct {
		name string
		got  APIErrorSpec
		want APIErrorSpec
	}{
		{
			name: "unauthorized",
			got:  unauthorizedErrorSpec(),
			want: globalErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "unauthorized"),
		},
		{
			name: "onboarding required",
			got:  onboardingRequiredErrorSpec(),
			want: globalErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "onboarding required"),
		},
		{
			name: "owner access required",
			got:  ownerAccessRequiredErrorSpec(),
			want: globalErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "owner access required"),
		},
		{
			name: "setup state load",
			got:  setupStateLoadErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load setup state"),
		},
		{
			name: "invalid month",
			got:  invalidMonthErrorSpec(),
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid month"),
		},
		{
			name: "not found",
			got:  notFoundErrorSpec(),
			want: globalErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "not found"),
		},
		{
			name: "template not found",
			got:  templateNotFoundErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "template not found"),
		},
		{
			name: "template render",
			got:  templateRenderErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to render template"),
		},
		{
			name: "partial not found",
			got:  partialNotFoundErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "partial not found"),
		},
		{
			name: "partial render",
			got:  partialRenderErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to render partial"),
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
