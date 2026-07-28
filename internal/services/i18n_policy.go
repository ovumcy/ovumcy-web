package services

import (
	"fmt"
	"strings"
)

var authErrorTranslationKeys = map[string]string{
	"invalid input":                  "auth.error.invalid_input",
	"consent required":               "auth.error.consent_required",
	"registration disabled":          "auth.error.registration_disabled",
	"invalid credentials":            "auth.error.invalid_credentials",
	"too many requests":              "common.error.too_many_requests",
	"common.error.too_many_requests": "common.error.too_many_requests",

	// Transport-level rejections. These specs are produced from the HTTP status
	// alone (internal/api/error_mapping_transport.go), by the top-level error
	// handler answering a *fiber.Error that no domain ever saw, so they have no
	// sentinel to map from and would otherwise render their machine key as the
	// visible message in every language.
	//
	// The list must cover EVERY key internal/api's transport table can produce,
	// including the two class fallbacks and request_timeout, which is the one
	// transport spec deliberately kept out of that table (503 already carries
	// service_unavailable). A key reaching this map is what turns the machine
	// key into copy; a key missing from it renders raw, which is how
	// request_timeout shipped as the literal text "request_timeout" in all six
	// languages. Regression:
	// api.TestEveryTransportErrorKeyRendersLocalizedCopyInEveryLocale.
	//
	// Two of them point at copy that already existed rather than at new strings:
	// 413 and 404 keep the wording their own surfaces already show —
	// not_found.title is exactly what respondNotFoundMappedError renders for an
	// HTMX 404, so one status keeps one phrasing whichever layer produced it.
	"bad_request":               "common.error.bad_request",
	"unauthorized":              "common.error.unauthorized",
	"forbidden":                 "common.error.forbidden",
	"not found":                 "not_found.title",
	"method_not_allowed":        "common.error.method_not_allowed",
	"request_too_large":         "common.error.request_too_large",
	"unsupported_media_type":    "common.error.unsupported_media_type",
	"request_headers_too_large": "common.error.request_headers_too_large",
	"request_rejected":          "common.error.request_rejected",
	"internal_error":            "common.error.internal_error",
	"service_unavailable":       "common.error.service_unavailable",
	"request_timeout":           "common.error.request_timeout",

	"email already exists":        "auth.error.email_exists",
	"register pickup unavailable": "auth.error.post_register_signin",
	"weak password":               "auth.error.weak_password",
	"password mismatch":           "auth.error.password_mismatch",
	"invalid recovery code":       "auth.error.invalid_recovery_code",
	"too many recovery attempts":  "auth.error.too_many_recovery_attempts",
	// The 2FA challenge's locale entries live under the flat error.totp_* namespace
	// (present in all six locales), unlike the auth.error.* keys around them. Kept
	// under their own names rather than renamed: the translations are correct and
	// the defect was that nothing mapped the spec keys onto them, so a wrong 2FA
	// code rendered the raw English "totp invalid code" in every language.
	"totp invalid code":      "error.totp_invalid_code",
	"totp session expired":   "error.totp_session_expired",
	"totp internal error":    "error.totp_internal_error",
	"totp too many attempts": "error.totp_too_many_attempts",

	"sso temporarily unavailable":                     "auth.error.sso_temporarily_unavailable",
	"sso authentication failed":                       "auth.error.sso_authentication_failed",
	"sso sign-in unavailable":                         "auth.error.sso_sign_in_unavailable",
	"sso link confirmation expired":                   "auth.error.sso_link_confirmation_expired",
	"sso link confirmation invalid password":          "auth.error.sso_link_confirmation_invalid_password",
	"sso link confirmation unavailable":               "auth.error.sso_link_confirmation_unavailable",
	"web sign-in unavailable":                         "auth.error.web_sign_in_unavailable",
	"local sign-in unavailable":                       "auth.error.local_sign_in_unavailable",
	"local recovery unavailable":                      "auth.error.local_recovery_unavailable",
	"too_many_sso_attempts":                           "auth.error.too_many_sso_attempts",
	"too many sso attempts":                           "auth.error.too_many_sso_attempts",
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
	"display name contains invalid characters":        "settings.error.display_name_invalid_characters",
	"invalid cycle start date":                        "settings.error.invalid_last_period_start",
	"invalid cycle start day":                         "dashboard.error.invalid_cycle_start_date",
	"invalid password":                                "settings.error.invalid_password",
	"local password required":                         "settings.error.local_password_required",
	"invalid webhook url":                             "settings.error.invalid_webhook_url",
	"failed to update webhook settings":               "settings.error.webhook_update_failed",
	"failed to update calendar feed":                  "settings.error.calendar_feed_update_failed",
	"invalid symptom name":                            "settings.symptoms.error.name_required",
	"symptom name is required":                        "settings.symptoms.error.name_required",
	"symptom name is too long":                        "settings.symptoms.error.name_too_long",
	"symptom name contains invalid characters":        "settings.symptoms.error.invalid_characters",
	"invalid symptom color":                           "settings.symptoms.error.invalid_color",
	"symptom name already exists":                     "settings.symptoms.error.duplicate_name",
	"symptom not found":                               "settings.symptoms.error.not_found",
	"built-in symptom cannot be edited":               "settings.symptoms.error.builtin_edit_forbidden",
	"built-in symptom cannot be hidden":               "settings.symptoms.error.builtin_hide_forbidden",
	"built-in symptom cannot be restored":             "settings.symptoms.error.builtin_restore_forbidden",
	"failed to create symptom":                        "settings.symptoms.error.create_failed",
	"failed to update symptom":                        "settings.symptoms.error.update_failed",
	"failed to hide symptom":                          "settings.symptoms.error.hide_failed",
	"failed to restore symptom":                       "settings.symptoms.error.restore_failed",
	"invalid mood value":                              "dashboard.error.invalid_mood",
	"invalid sex activity value":                      "dashboard.error.invalid_sex_activity",
	"invalid bbt value":                               "dashboard.error.invalid_bbt",
	"invalid cervical mucus value":                    "dashboard.error.invalid_cervical_mucus",
	"invalid pregnancy test value":                    "dashboard.error.invalid_pregnancy_test",
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
	"password_changed":        "settings.success.password_changed",
	"cycle_updated":           "settings.success.cycle_updated",
	"reminders_updated":       "settings.success.reminders_updated",
	"interface_updated":       "settings.success.interface_updated",
	"tracking_updated":        "settings.success.tracking_updated",
	"webhook_updated":         "settings.success.webhook_updated",
	"calendar_feed_generated": "settings.success.calendar_feed_generated",
	"calendar_feed_rotated":   "settings.success.calendar_feed_rotated",
	"calendar_feed_revoked":   "settings.success.calendar_feed_revoked",
	"profile_updated":         "settings.success.profile_updated",
	"profile_name_cleared":    "settings.success.profile_name_cleared",
	"data_cleared":            "settings.success.data_cleared",
	"symptom_created":         "settings.symptoms.success.created",
	"symptom_updated":         "settings.symptoms.success.updated",
	"symptom_hidden":          "settings.symptoms.success.hidden",
	"symptom_restored":        "settings.symptoms.success.restored",
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

func LocalizedSymptomFrequencySummary(language string, count int, days int) string {
	lang := strings.ToLower(strings.TrimSpace(language))
	if lang == "ru" {
		return fmt.Sprintf("%d %s (за %d %s)",
			count,
			russianPluralForm(count, "раз", "раза", "раз"),
			days,
			russianPluralForm(days, "день", "дня", "дней"),
		)
	}
	if lang == "es" {
		countWord := "veces"
		if count == 1 {
			countWord = "vez"
		}
		dayWord := "días"
		if days == 1 {
			dayWord = "día"
		}
		return fmt.Sprintf("%d %s (en %d %s)", count, countWord, days, dayWord)
	}
	if lang == "de" {
		dayWord := "Tagen"
		if days == 1 {
			dayWord = "Tag"
		}
		return fmt.Sprintf("%d Mal (an %d %s)", count, days, dayWord)
	}
	if lang == "fr" {
		countWord := "fois"
		if count == 1 {
			countWord = "fois"
		}
		dayWord := "jours"
		if days == 1 {
			dayWord = "jour"
		}
		return fmt.Sprintf("%d %s (en %d %s)", count, countWord, days, dayWord)
	}

	if lang == "it" {
		countWord := "volte"
		if count == 1 {
			countWord = "volta"
		}
		dayWord := "giorni"
		if days == 1 {
			dayWord = "giorno"
		}
		return fmt.Sprintf("%d %s (in %d %s)", count, countWord, days, dayWord)
	}

	countWord := "times"
	if count == 1 {
		countWord = "time"
	}
	dayWord := "days"
	if days == 1 {
		dayWord = "day"
	}
	return fmt.Sprintf("%d %s (in %d %s)", count, countWord, days, dayWord)
}

func russianPluralForm(value int, one string, few string, many string) string {
	absolute := value
	if absolute < 0 {
		absolute = -absolute
	}
	lastTwoDigits := absolute % 100
	if lastTwoDigits >= 11 && lastTwoDigits <= 14 {
		return many
	}

	lastDigit := absolute % 10
	switch {
	case lastDigit == 1:
		return one
	case lastDigit >= 2 && lastDigit <= 4:
		return few
	default:
		return many
	}
}
