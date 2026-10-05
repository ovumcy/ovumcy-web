package api

var pageTemplates = []string{
	"login",
	"register",
	"recovery_code",
	"forgot_password",
	"reset_password",
	"onboarding",
	"dashboard",
	"calendar",
	"stats",
	"settings",
	"settings_2fa",
	"calendar_feed_reveal",
	"auth_2fa",
	"not_found",
	"privacy",
	pageFormRefusalTemplate,
}

var partialTemplateFiles = []string{
	"day_editor_partial.html",
	"current_user_identity_oob.html",
	"settings_symptoms_section.html",
	"settings_egress_section.html",
}
