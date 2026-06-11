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

type NotificationService struct{}

func NewNotificationService() *NotificationService {
	return &NotificationService{}
}

func (service *NotificationService) ResolveSettingsStatus(flashSuccess string) string {
	return firstNonEmptyTrimmed(flashSuccess)
}

func (service *NotificationService) ResolveSettingsErrorSource(flashError string) string {
	return firstNonEmptyTrimmed(flashError)
}

func (service *NotificationService) ClassifySettingsErrorSource(errorSource string) SettingsErrorTarget {
	if service.IsChangePasswordErrorMessage(errorSource) {
		return SettingsErrorTargetChangePassword
	}
	return SettingsErrorTargetGeneral
}

func (service *NotificationService) IsChangePasswordErrorMessage(message string) bool {
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
