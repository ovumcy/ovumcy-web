package api

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
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
			name: "local password required",
			got:  settingsLocalPasswordRequiredErrorSpec(),
			want: settingsFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "local password required"),
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
			name: "display name invalid characters",
			err:  services.ErrSettingsDisplayNameInvalidCharacters,
			want: settingsValidationErrorSpec("display name contains invalid characters"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: settingsValidationErrorSpec("invalid profile input"),
		},
	}

	for _, testCase := range testCases {

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
			// WEB-54: no local password answers IDENTICALLY to a wrong one —
			// same spec as "invalid password" above, not the distinct
			// "local password required" 403 this used to map to.
			name: "no local password merges into invalid password",
			err:  services.ErrSettingsLocalPasswordNotSet,
			want: settingsInvalidPasswordErrorSpec(),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: settingsValidatePasswordErrorSpec(),
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			if got := mapSettingsDeleteAccountPasswordError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

// TestSettingsReauthMergesNoLocalPasswordIntoInvalidPassword is WEB-54's core
// regression: the caller-visible refusal for "this account has no local
// password" must be byte-identical — same status, same key, same category,
// same target — to the refusal for "that password is wrong", on every
// settings endpoint that re-authenticates by password. Before this fix the
// two mappers below answered ErrSettingsLocalPasswordNotSet with a distinct
// 403 "local password required" spec that a wrong password never produced;
// ValidateCurrentPassword/ValidatePasswordChange already equalize the two
// refusals' bcrypt cost (SEC-L3, WEB-13), so status/key was the only
// remaining oracle. The distinction survives only in the security log
// (logSecurityError is passed the original typed error, never the mapped
// spec's key).
//
// mapOIDCIdentityUnlinkError's reuse of settingsLocalPasswordRequiredErrorSpec
// for ErrOIDCUnlinkLastSignIn is deliberately NOT covered here: that refusal
// fires only after the caller has already proved the current password (a
// business rule on removing the account's last sign-in method), so it carries
// no re-auth oracle and is out of this test's scope by design.
func TestSettingsReauthMergesNoLocalPasswordIntoInvalidPassword(t *testing.T) {
	t.Run("clear-data/delete-account/2fa-disable/oidc-link/oidc-unlink/recovery-code", func(t *testing.T) {
		wrongPassword := mapSettingsDeleteAccountPasswordError(services.ErrSettingsPasswordInvalid)
		noLocalPassword := mapSettingsDeleteAccountPasswordError(services.ErrSettingsLocalPasswordNotSet)
		if wrongPassword != noLocalPassword {
			t.Fatalf("wrong password and no local password must answer identically: wrong=%#v noLocalPassword=%#v", wrongPassword, noLocalPassword)
		}
		if wrongPassword.Status != fiber.StatusUnauthorized {
			t.Fatalf("expected the merged refusal to stay 401, got %d", wrongPassword.Status)
		}
	})

	t.Run("password change", func(t *testing.T) {
		wrongPassword := mapSettingsPasswordChangeError(services.ErrSettingsInvalidCurrentPassword)
		noLocalPassword := mapSettingsPasswordChangeError(services.ErrSettingsLocalPasswordNotSet)
		if wrongPassword != noLocalPassword {
			t.Fatalf("wrong current password and no local password must answer identically: wrong=%#v noLocalPassword=%#v", wrongPassword, noLocalPassword)
		}
		if wrongPassword.Status != fiber.StatusUnauthorized {
			t.Fatalf("expected the merged refusal to stay 401, got %d", wrongPassword.Status)
		}
	})
}
