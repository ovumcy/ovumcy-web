package services

// invalidAuthFields names, per auth-page error key, the form fields the
// error is about, so the page can mark them aria-invalid and describe them by
// the announced error. A refusal that must not say which operand was wrong
// (invalid credentials, invalid recovery code) names every operand it
// compared, never one of them: the markup carries no more than the copy does.
// A key absent here marks no field — the error is announced, not attributed.
var invalidAuthFields = map[string][]string{
	"auth.error.invalid_credentials":              {"email", "password"},
	"auth.error.email_exists":                     {"email"},
	"auth.error.weak_password":                    {"password"},
	"auth.error.password_too_long":                {"password"},
	"auth.error.password_mismatch":                {"confirm_password"},
	"auth.error.consent_required":                 {"consent"},
	"auth.error.invalid_recovery_code":            {"recovery_code", "password"},
	"auth.error.recovery_reset_requires_password": {"password"},
	"error.totp_invalid_code":                     {"code"},
}

// InvalidAuthFields reports the form fields an auth-page error key marks
// invalid, keyed by field name. It always returns a non-nil map so a template
// can index it for any field without a presence check.
func InvalidAuthFields(errorKey string) map[string]bool {
	fields := map[string]bool{}
	for _, name := range invalidAuthFields[errorKey] {
		fields[name] = true
	}
	return fields
}
