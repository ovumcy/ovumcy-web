package api

import (
	"bytes"
	"log"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestSettingsReauthCauseFieldDiffersInTheLogWhileTheResponseDoesNot is WEB-54's
// follow-up regression: mapSettingsDeleteAccountPasswordError and
// mapSettingsPasswordChangeError merge "wrong password" and "no local
// password" into one byte-identical caller-visible refusal
// (TestSettingsReauthMergesNoLocalPasswordIntoInvalidPassword pins that at the
// mapper level, and TestOIDCOnlySettingsSensitiveActionsRequireLocalPassword
// end to end), but every doc and comment claiming the distinction "survives
// only in the security log" is only true if the log line actually carries it.
// This pins that: the recovery-code regeneration endpoint's audit line must
// carry a reauth_cause field distinguishing the two, taken from the raw
// service error before mapping (settingsReauthCauseField), while the HTTP
// response stays identical either way.
func TestSettingsReauthCauseFieldDiffersInTheLogWhileTheResponseDoesNot(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	wrongPasswordCtx := newSettingsSecurityTestContextWithOptions(t, "reauth-cause-wrong-password@example.com", onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})
	noLocalPasswordCtx := newOIDCOnlySettingsSecurityTestContextWithOptions(t, "reauth-cause-no-local-password@example.com", onboardingTestAppOptions{auditLogEnabled: true})

	var wrongPasswordLog bytes.Buffer
	log.SetOutput(&wrongPasswordLog)
	wrongPasswordResponse := settingsFormRequestWithCSRF(t, wrongPasswordCtx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{"password": {"NotTheRealPassword1"}}, map[string]string{
		"Accept": "application/json",
	})
	wrongPasswordStatus := wrongPasswordResponse.StatusCode
	wrongPasswordError := readAPIError(t, wrongPasswordResponse.Body)

	var noLocalPasswordLog bytes.Buffer
	log.SetOutput(&noLocalPasswordLog)
	noLocalPasswordResponse := settingsFormRequestWithCSRF(t, noLocalPasswordCtx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{"password": {"unused"}}, map[string]string{
		"Accept": "application/json",
	})
	noLocalPasswordStatus := noLocalPasswordResponse.StatusCode
	noLocalPasswordError := readAPIError(t, noLocalPasswordResponse.Body)

	// The response side of the regression: byte-identical status and key.
	if wrongPasswordStatus != http.StatusUnauthorized {
		t.Fatalf("expected the wrong-password refusal to stay 401, got %d", wrongPasswordStatus)
	}
	if noLocalPasswordStatus != wrongPasswordStatus {
		t.Fatalf("expected identical status codes: wrongPassword=%d noLocalPassword=%d", wrongPasswordStatus, noLocalPasswordStatus)
	}
	if noLocalPasswordError != wrongPasswordError {
		t.Fatalf("expected identical response error keys: wrongPassword=%q noLocalPassword=%q", wrongPasswordError, noLocalPasswordError)
	}

	// The log side: the two must be tellable apart via reauth_cause.
	wrongPasswordLine := securityEventLine(t, wrongPasswordLog.String(), "auth.recovery_code_regenerate", "denied")
	noLocalPasswordLine := securityEventLine(t, noLocalPasswordLog.String(), "auth.recovery_code_regenerate", "denied")

	if !strings.Contains(wrongPasswordLine, `reauth_cause="invalid_password"`) {
		t.Fatalf("expected the wrong-password log line to carry reauth_cause=invalid_password, got %q", wrongPasswordLine)
	}
	if !strings.Contains(noLocalPasswordLine, `reauth_cause="no_local_password"`) {
		t.Fatalf("expected the no-local-password log line to carry reauth_cause=no_local_password, got %q", noLocalPasswordLine)
	}
	if strings.Contains(wrongPasswordLine, `reauth_cause="no_local_password"`) {
		t.Fatalf("wrong-password log line must not also carry reauth_cause=no_local_password: %q", wrongPasswordLine)
	}
	if strings.Contains(noLocalPasswordLine, `reauth_cause="invalid_password"`) {
		t.Fatalf("no-local-password log line must not also carry reauth_cause=invalid_password: %q", noLocalPasswordLine)
	}
}

// TestPasswordChangeReauthCauseFieldDiffersInTheLogWhileTheResponseDoesNot is
// the password-change sibling of
// TestSettingsReauthCauseFieldDiffersInTheLogWhileTheResponseDoesNot: the
// recovery-code site pins the reauth_cause field for one caller of
// settingsReauthCauseField, but respondPasswordChangeError
// (handlers_settings_password.go) is a second, independent call site, and
// both of its merged arms — a wrong current password
// (services.ErrSettingsInvalidCurrentPassword) and an account with no local
// password at all (services.ErrSettingsLocalPasswordNotSet) — need their own
// proof, since a mutant dropping the field at this site would survive the
// recovery-code test alone. The no-local-password arm is reachable through
// PUT /api/v1/users/current/password only in the data state ValidatePasswordChange
// actually branches on (LocalAuthEnabled=true, PasswordHash="") — the more common
// OIDC-only case (LocalAuthEnabled=false) never reaches this mapper at all: it
// is refused earlier, by ChangePassword's own OIDC-reauth-required gate, with
// no reauth_cause field involved.
func TestPasswordChangeReauthCauseFieldDiffersInTheLogWhileTheResponseDoesNot(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	wrongPasswordCtx := newSettingsSecurityTestContextWithOptions(t, "pwchange-reauth-cause-wrong-password@example.com", onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})
	noLocalPasswordCtx := newLocalAuthEnabledEmptyHashSettingsTestContext(t, "pwchange-reauth-cause-no-local-password@example.com")

	var wrongPasswordLog bytes.Buffer
	log.SetOutput(&wrongPasswordLog)
	wrongPasswordResponse := settingsFormRequestWithCSRF(t, wrongPasswordCtx, http.MethodPut, "/api/v1/users/current/password", url.Values{
		"current_password": {"NotTheRealPassword1"},
		"new_password":     {"AnotherStrongPass2"},
		"confirm_password": {"AnotherStrongPass2"},
	}, map[string]string{
		"Accept": "application/json",
	})
	wrongPasswordStatus := wrongPasswordResponse.StatusCode
	wrongPasswordError := readAPIError(t, wrongPasswordResponse.Body)

	var noLocalPasswordLog bytes.Buffer
	log.SetOutput(&noLocalPasswordLog)
	noLocalPasswordResponse := settingsFormRequestWithCSRF(t, noLocalPasswordCtx, http.MethodPut, "/api/v1/users/current/password", url.Values{
		"current_password": {"unused"},
		"new_password":     {"AnotherStrongPass2"},
		"confirm_password": {"AnotherStrongPass2"},
	}, map[string]string{
		"Accept": "application/json",
	})
	noLocalPasswordStatus := noLocalPasswordResponse.StatusCode
	noLocalPasswordError := readAPIError(t, noLocalPasswordResponse.Body)

	// The response side of the regression: byte-identical status and key.
	if wrongPasswordStatus != http.StatusUnauthorized {
		t.Fatalf("expected the wrong-password refusal to stay 401, got %d", wrongPasswordStatus)
	}
	if noLocalPasswordStatus != wrongPasswordStatus {
		t.Fatalf("expected identical status codes: wrongPassword=%d noLocalPassword=%d", wrongPasswordStatus, noLocalPasswordStatus)
	}
	if noLocalPasswordError != wrongPasswordError {
		t.Fatalf("expected identical response error keys: wrongPassword=%q noLocalPassword=%q", wrongPasswordError, noLocalPasswordError)
	}

	// The log side: the two must be tellable apart via reauth_cause.
	wrongPasswordLine := securityEventLine(t, wrongPasswordLog.String(), "auth.password_change", "denied")
	noLocalPasswordLine := securityEventLine(t, noLocalPasswordLog.String(), "auth.password_change", "denied")

	if !strings.Contains(wrongPasswordLine, `reauth_cause="invalid_password"`) {
		t.Fatalf("expected the wrong-password log line to carry reauth_cause=invalid_password, got %q", wrongPasswordLine)
	}
	if !strings.Contains(noLocalPasswordLine, `reauth_cause="no_local_password"`) {
		t.Fatalf("expected the no-local-password log line to carry reauth_cause=no_local_password, got %q", noLocalPasswordLine)
	}
	if strings.Contains(wrongPasswordLine, `reauth_cause="no_local_password"`) {
		t.Fatalf("wrong-password log line must not also carry reauth_cause=no_local_password: %q", wrongPasswordLine)
	}
	if strings.Contains(noLocalPasswordLine, `reauth_cause="invalid_password"`) {
		t.Fatalf("no-local-password log line must not also carry reauth_cause=invalid_password: %q", noLocalPasswordLine)
	}
}
