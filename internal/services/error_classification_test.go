package services

import (
	"errors"
	"fmt"
	"testing"
)

func TestClassifyAuthRegisterError(t *testing.T) {
	tests := []struct {
		err  error
		want AuthRegisterErrorKind
	}{
		{err: ErrAuthRegisterInvalid, want: AuthRegisterErrorInvalidInput},
		{err: ErrAuthPasswordMismatch, want: AuthRegisterErrorPasswordMismatch},
		{err: ErrAuthWeakPassword, want: AuthRegisterErrorWeakPassword},
		{err: ErrAuthEmailExists, want: AuthRegisterErrorEmailExists},
		{err: ErrRegistrationSeedSymptoms, want: AuthRegisterErrorSeedSymptoms},
		{err: ErrAuthRegisterFailed, want: AuthRegisterErrorFailed},
		{err: errors.New("unknown"), want: AuthRegisterErrorUnknown},
	}

	for _, testCase := range tests {
		if got := ClassifyAuthRegisterError(testCase.err); got != testCase.want {
			t.Fatalf("ClassifyAuthRegisterError(%v) = %v, want %v", testCase.err, got, testCase.want)
		}
	}
}

func TestClassifyAuthAndRecoveryErrors(t *testing.T) {
	if got := ClassifyAuthLoginError(ErrAuthInvalidCreds); got != AuthLoginErrorInvalidCredentials {
		t.Fatalf("expected AuthLoginErrorInvalidCredentials, got %v", got)
	}
	if got := ClassifyAuthLoginError(ErrLoginResetTokenIssue); got != AuthLoginErrorResetTokenIssue {
		t.Fatalf("expected AuthLoginErrorResetTokenIssue, got %v", got)
	}
	if got := ClassifyPasswordRecoveryStartError(ErrPasswordRecoveryRateLimited); got != PasswordRecoveryStartErrorRateLimited {
		t.Fatalf("expected PasswordRecoveryStartErrorRateLimited, got %v", got)
	}
	if got := ClassifyPasswordRecoveryStartError(ErrPasswordRecoveryCodeInvalid); got != PasswordRecoveryStartErrorInvalidCode {
		t.Fatalf("expected PasswordRecoveryStartErrorInvalidCode, got %v", got)
	}
	if got := ClassifyPasswordResetCompleteError(ErrAuthResetInvalid); got != PasswordResetCompleteErrorInvalidInput {
		t.Fatalf("expected PasswordResetCompleteErrorInvalidInput, got %v", got)
	}
	if got := ClassifyPasswordResetCompleteError(ErrInvalidResetToken); got != PasswordResetCompleteErrorInvalidToken {
		t.Fatalf("expected PasswordResetCompleteErrorInvalidToken, got %v", got)
	}
}

func TestClassifyRangeAndDayErrors(t *testing.T) {
	if got := ClassifyDayRangeError(ErrDayRangeFromInvalid); got != DayRangeErrorFromInvalid {
		t.Fatalf("expected DayRangeErrorFromInvalid, got %v", got)
	}
	if got := ClassifyDayRangeError(ErrDayRangeToInvalid); got != DayRangeErrorToInvalid {
		t.Fatalf("expected DayRangeErrorToInvalid, got %v", got)
	}
	if got := ClassifyDayRangeError(ErrDayRangeInvalid); got != DayRangeErrorInvalid {
		t.Fatalf("expected DayRangeErrorInvalid, got %v", got)
	}

	if got := ClassifyDayUpsertError(ErrInvalidDayFlow); got != DayUpsertErrorInvalidFlow {
		t.Fatalf("expected DayUpsertErrorInvalidFlow, got %v", got)
	}
	if got := ClassifyDayUpsertError(ErrDayEntryCreateFailed); got != DayUpsertErrorCreateFailed {
		t.Fatalf("expected DayUpsertErrorCreateFailed, got %v", got)
	}
	if got := ClassifyDayUpsertError(ErrDayAutoFillApplyFailed); got != DayUpsertErrorUpdateFailed {
		t.Fatalf("expected DayUpsertErrorUpdateFailed, got %v", got)
	}
	if got := ClassifyDayDeleteError(ErrDeleteDayFailed); got != DayDeleteErrorDeleteFailed {
		t.Fatalf("expected DayDeleteErrorDeleteFailed, got %v", got)
	}
}

func TestClassifySymptomAndSettingsErrors(t *testing.T) {
	if got := ClassifySymptomCreateError(ErrInvalidSymptomName); got != SymptomCreateErrorInvalidName {
		t.Fatalf("expected SymptomCreateErrorInvalidName, got %v", got)
	}
	if got := ClassifySymptomDeleteError(fmt.Errorf("%w: anything", ErrCleanSymptomLogsFailed)); got != SymptomDeleteErrorCleanLogsFailed {
		t.Fatalf("expected SymptomDeleteErrorCleanLogsFailed, got %v", got)
	}
	if got := ClassifySettingsCycleValidationError(ErrSettingsPeriodLengthIncompatible); got != SettingsCycleValidationErrorPeriodLengthIncompatible {
		t.Fatalf("expected SettingsCycleValidationErrorPeriodLengthIncompatible, got %v", got)
	}
	if got := ClassifySettingsDeleteAccountPasswordError(ErrSettingsPasswordInvalid); got != SettingsDeleteAccountPasswordErrorInvalid {
		t.Fatalf("expected SettingsDeleteAccountPasswordErrorInvalid, got %v", got)
	}
	if got := ClassifySettingsPasswordChangeError(ErrSettingsPasswordHashFailed); got != SettingsPasswordChangeErrorHashFailed {
		t.Fatalf("expected SettingsPasswordChangeErrorHashFailed, got %v", got)
	}
	if got := ClassifySettingsProfileError(ErrSettingsDisplayNameTooLong); got != SettingsProfileErrorDisplayNameTooLong {
		t.Fatalf("expected SettingsProfileErrorDisplayNameTooLong, got %v", got)
	}
	if got := ClassifyRecoveryCodeRegenerationError(ErrRecoveryCodeUpdate); got != RecoveryCodeRegenerationErrorUpdateFailed {
		t.Fatalf("expected RecoveryCodeRegenerationErrorUpdateFailed, got %v", got)
	}
}

func TestClassifyOnboardingAndExportErrors(t *testing.T) {
	if got := ClassifyOnboardingStep1Error(ErrOnboardingStartDateRequired); got != OnboardingStep1ErrorDateRequired {
		t.Fatalf("expected OnboardingStep1ErrorDateRequired, got %v", got)
	}
	if got := ClassifyOnboardingCompletionError(ErrOnboardingCompletionNotNeeded); got != OnboardingCompletionErrorNotNeeded {
		t.Fatalf("expected OnboardingCompletionErrorNotNeeded, got %v", got)
	}
	if got := ClassifyExportRangeError(ErrExportToDateInvalid); got != ExportRangeErrorToInvalid {
		t.Fatalf("expected ExportRangeErrorToInvalid, got %v", got)
	}
}

func TestClassifyAuthSessionResolveError(t *testing.T) {
	if got := ClassifyAuthSessionResolveError(ErrAuthSessionTokenMissing); got != AuthSessionResolveErrorMissing {
		t.Fatalf("expected AuthSessionResolveErrorMissing, got %v", got)
	}
	if got := ClassifyAuthSessionResolveError(ErrAuthSessionTokenExpired); got != AuthSessionResolveErrorExpired {
		t.Fatalf("expected AuthSessionResolveErrorExpired, got %v", got)
	}
	if got := ClassifyAuthSessionResolveError(ErrAuthSessionTokenInvalidUserID); got != AuthSessionResolveErrorInvalid {
		t.Fatalf("expected AuthSessionResolveErrorInvalid, got %v", got)
	}
}
