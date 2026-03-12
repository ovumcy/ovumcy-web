package api

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestMapAuthRegisterError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "password mismatch",
			err:  services.ErrAuthPasswordMismatch,
			want: authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "password mismatch"),
		},
		{
			name: "weak password",
			err:  services.ErrAuthWeakPassword,
			want: authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "weak password"),
		},
		{
			name: "email exists",
			err:  services.ErrAuthEmailExists,
			want: authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input"),
		},
		{
			name: "seed symptoms",
			err:  services.ErrRegistrationSeedSymptoms,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to seed symptoms"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create account"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapAuthRegisterError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapAuthLoginError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "rate limited",
			err:  services.ErrAuthLoginRateLimited,
			want: authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many login attempts"),
		},
		{
			name: "invalid credentials",
			err:  services.ErrAuthInvalidCreds,
			want: authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials"),
		},
		{
			name: "reset token issue",
			err:  services.ErrLoginResetTokenIssue,
			want: authResetTokenCreateErrorSpec(),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapAuthLoginError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapPasswordRecoveryStartError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "rate limited",
			err:  services.ErrPasswordRecoveryRateLimited,
			want: authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many recovery attempts"),
		},
		{
			name: "invalid input",
			err:  services.ErrPasswordRecoveryInputInvalid,
			want: authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input"),
		},
		{
			name: "invalid code",
			err:  services.ErrPasswordRecoveryCodeInvalid,
			want: authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid recovery code"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: authResetTokenCreateErrorSpec(),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapPasswordRecoveryStartError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapPasswordResetCompleteError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "password mismatch",
			err:  services.ErrAuthPasswordMismatch,
			want: authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "password mismatch"),
		},
		{
			name: "weak password",
			err:  services.ErrAuthWeakPassword,
			want: authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "weak password"),
		},
		{
			name: "invalid input",
			err:  services.ErrAuthResetInvalid,
			want: authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input"),
		},
		{
			name: "invalid token",
			err:  services.ErrInvalidResetToken,
			want: authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid reset token"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to reset password"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapPasswordResetCompleteError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestAuthSessionErrorSpecs(t *testing.T) {
	if got := authSessionCreateErrorSpec(); got != globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create session") {
		t.Fatalf("unexpected create session error spec: %#v", got)
	}
	if got := authSessionRevokeErrorSpec(); got != globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to revoke session") {
		t.Fatalf("unexpected revoke session error spec: %#v", got)
	}
}
