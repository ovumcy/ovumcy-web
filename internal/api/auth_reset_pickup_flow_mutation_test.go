package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// This file hardens two auth-flow decisions against surviving mutants from
// gremlins baseline run 28758365493:
//   - handlers_auth_session_recovery.go L98: clearing the reset cookie only on
//     an invalid-reset-token completion error;
//   - handlers_auth_session_register_pickup.go L161: emitting the
//     redirect_signin security event with its reason on a failed pickup.

// TestResetPasswordClearsCookieOnInvalidResetToken pins the L98 guard
// `if spec.Key == "invalid reset token" { handler.clearResetPasswordCookie(c) }`.
// A tampered reset token maps to the invalid-reset-token spec, so the stale
// sealed cookie must be actively cleared (Set-Cookie with an empty value and a
// past expiry) rather than left on the browser to be retried. The
// CONDITIONALS_NEGATION mutant flips the comparison to `!=`, which skips the
// clear for exactly the invalid-token case, leaving the dead cookie in place.
func TestResetPasswordClearsCookieOnInvalidResetToken(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-invalid-clears-cookie@example.com", "StrongPass1", true)

	validToken := mustSignResetTokenForTest(t, user.ID, user.PasswordHash, time.Now().Add(10*time.Minute), time.Now())
	tamperedToken := mustTamperResetTokenSignatureForTest(t, validToken)
	resetCookieValue := mustSealResetCookieValueForTest(t, []byte("test-secret-key"), tamperedToken, false)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(url.Values{
		"password":         {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("reset-password request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}

	cleared := responseCookie(response.Cookies(), resetPasswordCookieName)
	if cleared == nil {
		t.Fatal("invalid reset token must clear the reset cookie; no Set-Cookie for it was emitted")
	}
	if strings.TrimSpace(cleared.Value) != "" {
		t.Fatalf("cleared reset cookie must carry an empty value, got %q", cleared.Value)
	}
	if !cleared.Expires.IsZero() && cleared.Expires.After(time.Now()) {
		t.Fatalf("cleared reset cookie must expire in the past, got expiry %s", cleared.Expires)
	}
}

// TestRegisterPickupFailureLogsRedirectSigninReason pins the L161 guard
// `if reason != "" { handler.logSecurityEvent(c, "auth.register_pickup",
// "redirect_signin", ...reason) }`. A pickup attempt with no cookie routes
// through redirectToPostRegisterSignin(c, "missing_or_expired"), which must emit
// the redirect_signin security event carrying that reason so operators can see
// why sessions bounce back to /login. The CONDITIONALS_NEGATION mutant flips the
// guard to `reason == ""`, suppressing the event for every real (non-empty)
// reason. Asserting the event is present fails under that mutation.
func TestRegisterPickupFailureLogsRedirectSigninReason(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{auditLogEnabled: true})

	// No pickup cookie -> redirectToPostRegisterSignin(c, "missing_or_expired").
	request := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register-welcome request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	logged := output.String()
	if !strings.Contains(logged, `action="auth.register_pickup"`) || !strings.Contains(logged, `outcome="redirect_signin"`) {
		t.Fatalf("expected a register_pickup redirect_signin security event, got %q", logged)
	}
	if !strings.Contains(logged, `reason="missing_or_expired"`) {
		t.Fatalf("expected the redirect_signin event to carry its reason, got %q", logged)
	}
}
