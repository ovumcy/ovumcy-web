package services

import "strings"

type SettingsErrorTarget string

const (
	SettingsErrorTargetGeneral        SettingsErrorTarget = "general"
	SettingsErrorTargetChangePassword SettingsErrorTarget = "change_password"
)

// SettingsPasswordChange* are the stable spec keys for password-change errors.
// They are used both as the API error key (in error_mapping_settings_password.go)
// and as the routing signal in ClassifySettingsErrorSource, so changing a
// display message only requires updating one constant instead of two separate
// switch statements.
const (
	SettingsPasswordChangeKeyInvalidInput     = "invalid settings input"
	SettingsPasswordChangeKeyPasswordMismatch = "password mismatch"
	SettingsPasswordChangeKeyInvalidCurrent   = "invalid current password"
	SettingsPasswordChangeKeyMustDiffer       = "new password must differ"
	SettingsPasswordChangeKeyWeakPassword     = "weak password"
)

// The functions below resolve settings flash messages into status keys and
// error-routing targets. They are pure string classification — no state, no
// dependencies — and live as plain policy functions like the rest of the
// *_policy.go files in this package (they used to be methods on a
// dependency-free NotificationService struct that cost a constructor param
// and a nil-guard for nothing).

func ResolveSettingsStatus(flashSuccess string) string {
	return firstNonEmptyTrimmed(flashSuccess)
}

func ResolveSettingsErrorSource(flashError string) string {
	return firstNonEmptyTrimmed(flashError)
}

func ClassifySettingsErrorSource(errorSource string) SettingsErrorTarget {
	if IsChangePasswordErrorMessage(errorSource) {
		return SettingsErrorTargetChangePassword
	}
	return SettingsErrorTargetGeneral
}

func IsChangePasswordErrorMessage(message string) bool {
	switch strings.ToLower(strings.TrimSpace(message)) {
	case SettingsPasswordChangeKeyInvalidInput,
		SettingsPasswordChangeKeyPasswordMismatch,
		SettingsPasswordChangeKeyInvalidCurrent,
		SettingsPasswordChangeKeyMustDiffer,
		SettingsPasswordChangeKeyWeakPassword:
		return true
	default:
		return false
	}
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
