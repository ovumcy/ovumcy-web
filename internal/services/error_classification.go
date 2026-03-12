package services

import "errors"

type AuthRegisterErrorKind uint8

const (
	AuthRegisterErrorUnknown AuthRegisterErrorKind = iota
	AuthRegisterErrorInvalidInput
	AuthRegisterErrorPasswordMismatch
	AuthRegisterErrorWeakPassword
	AuthRegisterErrorEmailExists
	AuthRegisterErrorSeedSymptoms
	AuthRegisterErrorFailed
)

func ClassifyAuthRegisterError(err error) AuthRegisterErrorKind {
	switch {
	case errors.Is(err, ErrAuthRegisterInvalid):
		return AuthRegisterErrorInvalidInput
	case errors.Is(err, ErrAuthPasswordMismatch):
		return AuthRegisterErrorPasswordMismatch
	case errors.Is(err, ErrAuthWeakPassword):
		return AuthRegisterErrorWeakPassword
	case errors.Is(err, ErrAuthEmailExists):
		return AuthRegisterErrorEmailExists
	case errors.Is(err, ErrRegistrationSeedSymptoms):
		return AuthRegisterErrorSeedSymptoms
	case errors.Is(err, ErrAuthRegisterFailed):
		return AuthRegisterErrorFailed
	default:
		return AuthRegisterErrorUnknown
	}
}

type AuthLoginErrorKind uint8

const (
	AuthLoginErrorUnknown AuthLoginErrorKind = iota
	AuthLoginErrorRateLimited
	AuthLoginErrorInvalidCredentials
	AuthLoginErrorResetTokenIssue
)

func ClassifyAuthLoginError(err error) AuthLoginErrorKind {
	switch {
	case errors.Is(err, ErrAuthLoginRateLimited):
		return AuthLoginErrorRateLimited
	case errors.Is(err, ErrAuthInvalidCreds):
		return AuthLoginErrorInvalidCredentials
	case errors.Is(err, ErrLoginResetTokenIssue):
		return AuthLoginErrorResetTokenIssue
	default:
		return AuthLoginErrorUnknown
	}
}

type PasswordRecoveryStartErrorKind uint8

const (
	PasswordRecoveryStartErrorUnknown PasswordRecoveryStartErrorKind = iota
	PasswordRecoveryStartErrorRateLimited
	PasswordRecoveryStartErrorInvalidInput
	PasswordRecoveryStartErrorInvalidCode
)

func ClassifyPasswordRecoveryStartError(err error) PasswordRecoveryStartErrorKind {
	switch {
	case errors.Is(err, ErrPasswordRecoveryRateLimited):
		return PasswordRecoveryStartErrorRateLimited
	case errors.Is(err, ErrPasswordRecoveryInputInvalid):
		return PasswordRecoveryStartErrorInvalidInput
	case errors.Is(err, ErrPasswordRecoveryCodeInvalid):
		return PasswordRecoveryStartErrorInvalidCode
	default:
		return PasswordRecoveryStartErrorUnknown
	}
}

type PasswordResetCompleteErrorKind uint8

const (
	PasswordResetCompleteErrorUnknown PasswordResetCompleteErrorKind = iota
	PasswordResetCompleteErrorInvalidInput
	PasswordResetCompleteErrorPasswordMismatch
	PasswordResetCompleteErrorWeakPassword
	PasswordResetCompleteErrorInvalidToken
)

func ClassifyPasswordResetCompleteError(err error) PasswordResetCompleteErrorKind {
	switch {
	case errors.Is(err, ErrAuthResetInvalid):
		return PasswordResetCompleteErrorInvalidInput
	case errors.Is(err, ErrAuthPasswordMismatch):
		return PasswordResetCompleteErrorPasswordMismatch
	case errors.Is(err, ErrAuthWeakPassword):
		return PasswordResetCompleteErrorWeakPassword
	case errors.Is(err, ErrInvalidResetToken):
		return PasswordResetCompleteErrorInvalidToken
	default:
		return PasswordResetCompleteErrorUnknown
	}
}

type AuthSessionResolveErrorKind uint8

const (
	AuthSessionResolveErrorUnknown AuthSessionResolveErrorKind = iota
	AuthSessionResolveErrorMissing
	AuthSessionResolveErrorExpired
	AuthSessionResolveErrorInvalid
)

func ClassifyAuthSessionResolveError(err error) AuthSessionResolveErrorKind {
	switch {
	case errors.Is(err, ErrAuthSessionTokenMissing):
		return AuthSessionResolveErrorMissing
	case errors.Is(err, ErrAuthSessionTokenExpired):
		return AuthSessionResolveErrorExpired
	case errors.Is(err, ErrAuthSessionTokenInvalid),
		errors.Is(err, ErrAuthSessionTokenInvalidUserID),
		errors.Is(err, ErrAuthSessionTokenRevoked),
		errors.Is(err, ErrAuthInvalidCreds):
		return AuthSessionResolveErrorInvalid
	default:
		return AuthSessionResolveErrorUnknown
	}
}

type DayRangeErrorKind uint8

const (
	DayRangeErrorUnknown DayRangeErrorKind = iota
	DayRangeErrorFromInvalid
	DayRangeErrorToInvalid
	DayRangeErrorInvalid
)

func ClassifyDayRangeError(err error) DayRangeErrorKind {
	switch {
	case errors.Is(err, ErrDayRangeFromInvalid):
		return DayRangeErrorFromInvalid
	case errors.Is(err, ErrDayRangeToInvalid):
		return DayRangeErrorToInvalid
	case errors.Is(err, ErrDayRangeInvalid):
		return DayRangeErrorInvalid
	default:
		return DayRangeErrorUnknown
	}
}

type DayUpsertErrorKind uint8

const (
	DayUpsertErrorUnknown DayUpsertErrorKind = iota
	DayUpsertErrorInvalidFlow
	DayUpsertErrorInvalidMood
	DayUpsertErrorInvalidSexActivity
	DayUpsertErrorInvalidBBT
	DayUpsertErrorInvalidCervicalMucus
	DayUpsertErrorLoadFailed
	DayUpsertErrorCreateFailed
	DayUpsertErrorUpdateFailed
	DayUpsertErrorSyncLastPeriodFailed
)

func ClassifyDayUpsertError(err error) DayUpsertErrorKind {
	switch {
	case errors.Is(err, ErrInvalidDayFlow):
		return DayUpsertErrorInvalidFlow
	case errors.Is(err, ErrInvalidDayMood):
		return DayUpsertErrorInvalidMood
	case errors.Is(err, ErrInvalidDaySexActivity):
		return DayUpsertErrorInvalidSexActivity
	case errors.Is(err, ErrInvalidDayBBT):
		return DayUpsertErrorInvalidBBT
	case errors.Is(err, ErrInvalidDayCervicalMucus):
		return DayUpsertErrorInvalidCervicalMucus
	case errors.Is(err, ErrDayAutoFillLoadFailed),
		errors.Is(err, ErrDayAutoFillCheckFailed),
		errors.Is(err, ErrDayEntryLoadFailed):
		return DayUpsertErrorLoadFailed
	case errors.Is(err, ErrDayEntryCreateFailed):
		return DayUpsertErrorCreateFailed
	case errors.Is(err, ErrDayAutoFillApplyFailed),
		errors.Is(err, ErrDayEntryUpdateFailed):
		return DayUpsertErrorUpdateFailed
	case errors.Is(err, ErrSyncLastPeriodFailed):
		return DayUpsertErrorSyncLastPeriodFailed
	default:
		return DayUpsertErrorUnknown
	}
}

type DayDeleteErrorKind uint8

const (
	DayDeleteErrorUnknown DayDeleteErrorKind = iota
	DayDeleteErrorDeleteFailed
	DayDeleteErrorSyncLastPeriodFailed
)

func ClassifyDayDeleteError(err error) DayDeleteErrorKind {
	switch {
	case errors.Is(err, ErrDeleteDayFailed):
		return DayDeleteErrorDeleteFailed
	case errors.Is(err, ErrSyncLastPeriodFailed):
		return DayDeleteErrorSyncLastPeriodFailed
	default:
		return DayDeleteErrorUnknown
	}
}

type SymptomCreateErrorKind uint8

const (
	SymptomCreateErrorUnknown SymptomCreateErrorKind = iota
	SymptomCreateErrorNameRequired
	SymptomCreateErrorNameTooLong
	SymptomCreateErrorNameInvalidCharacters
	SymptomCreateErrorInvalidColor
	SymptomCreateErrorDuplicateName
	SymptomCreateErrorFailed
)

func ClassifySymptomCreateError(err error) SymptomCreateErrorKind {
	switch {
	case errors.Is(err, ErrSymptomNameRequired):
		return SymptomCreateErrorNameRequired
	case errors.Is(err, ErrSymptomNameTooLong):
		return SymptomCreateErrorNameTooLong
	case errors.Is(err, ErrSymptomNameInvalidCharacters):
		return SymptomCreateErrorNameInvalidCharacters
	case errors.Is(err, ErrInvalidSymptomColor):
		return SymptomCreateErrorInvalidColor
	case errors.Is(err, ErrSymptomNameAlreadyExists):
		return SymptomCreateErrorDuplicateName
	case errors.Is(err, ErrCreateSymptomFailed):
		return SymptomCreateErrorFailed
	default:
		return SymptomCreateErrorUnknown
	}
}

type SymptomUpdateErrorKind uint8

const (
	SymptomUpdateErrorUnknown SymptomUpdateErrorKind = iota
	SymptomUpdateErrorNotFound
	SymptomUpdateErrorNameRequired
	SymptomUpdateErrorNameTooLong
	SymptomUpdateErrorNameInvalidCharacters
	SymptomUpdateErrorInvalidColor
	SymptomUpdateErrorDuplicateName
	SymptomUpdateErrorBuiltinForbidden
	SymptomUpdateErrorFailed
)

func ClassifySymptomUpdateError(err error) SymptomUpdateErrorKind {
	switch {
	case errors.Is(err, ErrSymptomNotFound):
		return SymptomUpdateErrorNotFound
	case errors.Is(err, ErrSymptomNameRequired):
		return SymptomUpdateErrorNameRequired
	case errors.Is(err, ErrSymptomNameTooLong):
		return SymptomUpdateErrorNameTooLong
	case errors.Is(err, ErrSymptomNameInvalidCharacters):
		return SymptomUpdateErrorNameInvalidCharacters
	case errors.Is(err, ErrInvalidSymptomColor):
		return SymptomUpdateErrorInvalidColor
	case errors.Is(err, ErrSymptomNameAlreadyExists):
		return SymptomUpdateErrorDuplicateName
	case errors.Is(err, ErrBuiltinSymptomEditForbidden):
		return SymptomUpdateErrorBuiltinForbidden
	case errors.Is(err, ErrUpdateSymptomFailed):
		return SymptomUpdateErrorFailed
	default:
		return SymptomUpdateErrorUnknown
	}
}

type SymptomArchiveErrorKind uint8

const (
	SymptomArchiveErrorUnknown SymptomArchiveErrorKind = iota
	SymptomArchiveErrorNotFound
	SymptomArchiveErrorBuiltinForbidden
	SymptomArchiveErrorFailed
)

func ClassifySymptomArchiveError(err error) SymptomArchiveErrorKind {
	switch {
	case errors.Is(err, ErrSymptomNotFound):
		return SymptomArchiveErrorNotFound
	case errors.Is(err, ErrBuiltinSymptomHideForbidden):
		return SymptomArchiveErrorBuiltinForbidden
	case errors.Is(err, ErrArchiveSymptomFailed):
		return SymptomArchiveErrorFailed
	default:
		return SymptomArchiveErrorUnknown
	}
}

type SymptomRestoreErrorKind uint8

const (
	SymptomRestoreErrorUnknown SymptomRestoreErrorKind = iota
	SymptomRestoreErrorNotFound
	SymptomRestoreErrorBuiltinForbidden
	SymptomRestoreErrorDuplicateName
	SymptomRestoreErrorFailed
)

func ClassifySymptomRestoreError(err error) SymptomRestoreErrorKind {
	switch {
	case errors.Is(err, ErrSymptomNotFound):
		return SymptomRestoreErrorNotFound
	case errors.Is(err, ErrBuiltinSymptomShowForbidden):
		return SymptomRestoreErrorBuiltinForbidden
	case errors.Is(err, ErrSymptomNameAlreadyExists):
		return SymptomRestoreErrorDuplicateName
	case errors.Is(err, ErrRestoreSymptomFailed):
		return SymptomRestoreErrorFailed
	default:
		return SymptomRestoreErrorUnknown
	}
}

type ExportRangeErrorKind uint8

const (
	ExportRangeErrorUnknown ExportRangeErrorKind = iota
	ExportRangeErrorFromInvalid
	ExportRangeErrorToInvalid
	ExportRangeErrorInvalid
)

func ClassifyExportRangeError(err error) ExportRangeErrorKind {
	switch {
	case errors.Is(err, ErrExportFromDateInvalid):
		return ExportRangeErrorFromInvalid
	case errors.Is(err, ErrExportToDateInvalid):
		return ExportRangeErrorToInvalid
	case errors.Is(err, ErrExportRangeInvalid):
		return ExportRangeErrorInvalid
	default:
		return ExportRangeErrorUnknown
	}
}

type OnboardingStep1ErrorKind uint8

const (
	OnboardingStep1ErrorUnknown OnboardingStep1ErrorKind = iota
	OnboardingStep1ErrorDateRequired
	OnboardingStep1ErrorDateInvalid
	OnboardingStep1ErrorDateOutOfRange
)

func ClassifyOnboardingStep1Error(err error) OnboardingStep1ErrorKind {
	switch {
	case errors.Is(err, ErrOnboardingStartDateRequired):
		return OnboardingStep1ErrorDateRequired
	case errors.Is(err, ErrOnboardingStartDateInvalid):
		return OnboardingStep1ErrorDateInvalid
	case errors.Is(err, ErrOnboardingStartDateOutOfRange):
		return OnboardingStep1ErrorDateOutOfRange
	default:
		return OnboardingStep1ErrorUnknown
	}
}

type OnboardingCompletionErrorKind uint8

const (
	OnboardingCompletionErrorUnknown OnboardingCompletionErrorKind = iota
	OnboardingCompletionErrorNotNeeded
	OnboardingCompletionErrorStepsRequired
)

func ClassifyOnboardingCompletionError(err error) OnboardingCompletionErrorKind {
	switch {
	case errors.Is(err, ErrOnboardingCompletionNotNeeded):
		return OnboardingCompletionErrorNotNeeded
	case errors.Is(err, ErrOnboardingStepsRequired):
		return OnboardingCompletionErrorStepsRequired
	default:
		return OnboardingCompletionErrorUnknown
	}
}

type SettingsCycleValidationErrorKind uint8

const (
	SettingsCycleValidationErrorUnknown SettingsCycleValidationErrorKind = iota
	SettingsCycleValidationErrorCycleLengthOutOfRange
	SettingsCycleValidationErrorPeriodLengthOutOfRange
	SettingsCycleValidationErrorPeriodLengthIncompatible
	SettingsCycleValidationErrorCycleStartDateInvalid
)

func ClassifySettingsCycleValidationError(err error) SettingsCycleValidationErrorKind {
	switch {
	case errors.Is(err, ErrSettingsCycleLengthOutOfRange):
		return SettingsCycleValidationErrorCycleLengthOutOfRange
	case errors.Is(err, ErrSettingsPeriodLengthOutOfRange):
		return SettingsCycleValidationErrorPeriodLengthOutOfRange
	case errors.Is(err, ErrSettingsPeriodLengthIncompatible):
		return SettingsCycleValidationErrorPeriodLengthIncompatible
	case errors.Is(err, ErrSettingsCycleStartDateInvalid):
		return SettingsCycleValidationErrorCycleStartDateInvalid
	default:
		return SettingsCycleValidationErrorUnknown
	}
}

type SettingsDeleteAccountPasswordErrorKind uint8

const (
	SettingsDeleteAccountPasswordErrorUnknown SettingsDeleteAccountPasswordErrorKind = iota
	SettingsDeleteAccountPasswordErrorMissing
	SettingsDeleteAccountPasswordErrorInvalid
)

func ClassifySettingsDeleteAccountPasswordError(err error) SettingsDeleteAccountPasswordErrorKind {
	switch {
	case errors.Is(err, ErrSettingsPasswordMissing):
		return SettingsDeleteAccountPasswordErrorMissing
	case errors.Is(err, ErrSettingsPasswordInvalid):
		return SettingsDeleteAccountPasswordErrorInvalid
	default:
		return SettingsDeleteAccountPasswordErrorUnknown
	}
}

type SettingsPasswordChangeErrorKind uint8

const (
	SettingsPasswordChangeErrorUnknown SettingsPasswordChangeErrorKind = iota
	SettingsPasswordChangeErrorInvalidInput
	SettingsPasswordChangeErrorPasswordMismatch
	SettingsPasswordChangeErrorInvalidCurrentPassword
	SettingsPasswordChangeErrorNewPasswordMustDiffer
	SettingsPasswordChangeErrorWeakPassword
	SettingsPasswordChangeErrorHashFailed
	SettingsPasswordChangeErrorUpdateFailed
)

func ClassifySettingsPasswordChangeError(err error) SettingsPasswordChangeErrorKind {
	switch {
	case errors.Is(err, ErrSettingsPasswordChangeInvalidInput):
		return SettingsPasswordChangeErrorInvalidInput
	case errors.Is(err, ErrSettingsPasswordMismatch):
		return SettingsPasswordChangeErrorPasswordMismatch
	case errors.Is(err, ErrSettingsInvalidCurrentPassword):
		return SettingsPasswordChangeErrorInvalidCurrentPassword
	case errors.Is(err, ErrSettingsNewPasswordMustDiffer):
		return SettingsPasswordChangeErrorNewPasswordMustDiffer
	case errors.Is(err, ErrSettingsWeakPassword):
		return SettingsPasswordChangeErrorWeakPassword
	case errors.Is(err, ErrSettingsPasswordHashFailed):
		return SettingsPasswordChangeErrorHashFailed
	case errors.Is(err, ErrSettingsPasswordUpdateFailed):
		return SettingsPasswordChangeErrorUpdateFailed
	default:
		return SettingsPasswordChangeErrorUnknown
	}
}

type SettingsProfileErrorKind uint8

const (
	SettingsProfileErrorUnknown SettingsProfileErrorKind = iota
	SettingsProfileErrorDisplayNameTooLong
)

func ClassifySettingsProfileError(err error) SettingsProfileErrorKind {
	switch {
	case errors.Is(err, ErrSettingsDisplayNameTooLong):
		return SettingsProfileErrorDisplayNameTooLong
	default:
		return SettingsProfileErrorUnknown
	}
}

type RecoveryCodeRegenerationErrorKind uint8

const (
	RecoveryCodeRegenerationErrorUnknown RecoveryCodeRegenerationErrorKind = iota
	RecoveryCodeRegenerationErrorGenerateFailed
	RecoveryCodeRegenerationErrorUpdateFailed
)

func ClassifyRecoveryCodeRegenerationError(err error) RecoveryCodeRegenerationErrorKind {
	switch {
	case errors.Is(err, ErrRecoveryCodeGenerate):
		return RecoveryCodeRegenerationErrorGenerateFailed
	case errors.Is(err, ErrRecoveryCodeUpdate):
		return RecoveryCodeRegenerationErrorUpdateFailed
	default:
		return RecoveryCodeRegenerationErrorUnknown
	}
}

type CalendarViewErrorKind uint8

const (
	CalendarViewErrorUnknown CalendarViewErrorKind = iota
	CalendarViewErrorLoadLogs
)

func ClassifyCalendarViewError(err error) CalendarViewErrorKind {
	switch {
	case errors.Is(err, ErrCalendarViewLoadLogs):
		return CalendarViewErrorLoadLogs
	default:
		return CalendarViewErrorUnknown
	}
}

type DashboardViewErrorKind uint8

const (
	DashboardViewErrorUnknown DashboardViewErrorKind = iota
	DashboardViewErrorLoadTodayLog
	DashboardViewErrorLoadDayState
	DashboardViewErrorLoadDayLog
	DashboardViewErrorLoadLogs
)

func ClassifyDashboardViewError(err error) DashboardViewErrorKind {
	switch {
	case errors.Is(err, ErrDashboardViewLoadTodayLog):
		return DashboardViewErrorLoadTodayLog
	case errors.Is(err, ErrDashboardViewLoadDayState):
		return DashboardViewErrorLoadDayState
	case errors.Is(err, ErrDashboardViewLoadDayLog):
		return DashboardViewErrorLoadDayLog
	case errors.Is(err, ErrDashboardViewLoadLogs):
		return DashboardViewErrorLoadLogs
	default:
		return DashboardViewErrorUnknown
	}
}

type StatsPageViewErrorKind uint8

const (
	StatsPageViewErrorUnknown StatsPageViewErrorKind = iota
	StatsPageViewErrorLoadSymptoms
)

func ClassifyStatsPageViewError(err error) StatsPageViewErrorKind {
	switch {
	case errors.Is(err, ErrStatsPageViewLoadSymptoms):
		return StatsPageViewErrorLoadSymptoms
	default:
		return StatsPageViewErrorUnknown
	}
}
