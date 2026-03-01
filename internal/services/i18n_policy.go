package services

import "strings"

var authErrorTranslationKeys = map[string]string{
	"invalid input":                                   "auth.error.invalid_input",
	"registration disabled":                           "auth.error.registration_disabled",
	"invalid credentials":                             "auth.error.invalid_credentials",
	"email already exists":                            "auth.error.email_exists",
	"weak password":                                   "auth.error.weak_password",
	"password mismatch":                               "auth.error.password_mismatch",
	"invalid recovery code":                           "auth.error.invalid_recovery_code",
	"too many recovery attempts":                      "auth.error.too_many_recovery_attempts",
	"too_many_login_attempts":                         "auth.error.too_many_login_attempts",
	"too many login attempts":                         "auth.error.too_many_login_attempts",
	"too_many_forgot_password_attempts":               "auth.error.too_many_forgot_password_attempts",
	"too many forgot password attempts":               "auth.error.too_many_forgot_password_attempts",
	"invalid reset token":                             "auth.error.invalid_reset_token",
	"invalid current password":                        "settings.error.invalid_current_password",
	"new password must differ":                        "settings.error.password_unchanged",
	"invalid settings input":                          "settings.error.invalid_input",
	"invalid profile input":                           "settings.error.invalid_profile_input",
	"display name too long":                           "settings.error.display_name_too_long",
	"invalid cycle start date":                        "settings.error.invalid_last_period_start",
	"invalid password":                                "settings.error.invalid_password",
	"period flow is required":                         "calendar.error.period_flow_required",
	"date is required":                                "onboarding.error.date_required",
	"invalid last period start":                       "onboarding.error.invalid_last_period_start",
	"last period start must be within last 60 days":   "onboarding.error.last_period_range",
	"cycle length must be between 15 and 90":          "onboarding.error.cycle_length_range",
	"period length must be between 1 and 14":          "onboarding.error.period_length_range",
	"period length is incompatible with cycle length": "settings.cycle.error_incompatible",
	"complete onboarding steps first":                 "onboarding.error.incomplete",
	"failed to save onboarding step":                  "onboarding.error.generic",
	"failed to finish onboarding":                     "onboarding.error.generic",
}

var settingsStatusTranslationKeys = map[string]string{
	"password_changed":     "settings.success.password_changed",
	"cycle_updated":        "settings.success.cycle_updated",
	"profile_updated":      "settings.success.profile_updated",
	"profile_name_cleared": "settings.success.profile_name_cleared",
	"data_cleared":         "settings.success.data_cleared",
}

var builtinSymptomTranslationKeys = map[string]string{
	"acne":              "symptoms.acne",
	"back pain":         "symptom.back_pain",
	"bloating":          "symptoms.bloating",
	"breast tenderness": "symptoms.breast_tenderness",
	"constipation":      "symptom.constipation",
	"cramps":            "symptoms.cramps",
	"diarrhea":          "symptom.diarrhea",
	"fatigue":           "symptoms.fatigue",
	"food cravings":     "symptom.food_cravings",
	"headache":          "symptoms.headache",
	"insomnia":          "symptom.insomnia",
	"irritability":      "symptom.irritability",
	"mood swings":       "symptoms.mood_swings",
	"nausea":            "symptom.nausea",
	"spotting":          "symptom.spotting",
	"swelling":          "symptom.swelling",
}

func AuthErrorTranslationKey(message string) string {
	key, ok := authErrorTranslationKeys[strings.ToLower(strings.TrimSpace(message))]
	if !ok {
		return ""
	}
	return key
}

func SettingsStatusTranslationKey(status string) string {
	key, ok := settingsStatusTranslationKeys[strings.ToLower(strings.TrimSpace(status))]
	if !ok {
		return ""
	}
	return key
}

func BuiltinSymptomTranslationKey(name string) string {
	key, ok := builtinSymptomTranslationKeys[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return ""
	}
	return key
}
