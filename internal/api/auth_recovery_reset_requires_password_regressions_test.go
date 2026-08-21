package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestRecoveryResetRefusesWithoutTheAccountPassword pins the two-factor shape of
// POST /api/v1/password-resets: the recovery code substitutes for the SECOND
// factor only. A submission carrying a valid email and a valid recovery code but
// no password — or a wrong password — must mint no reset token and issue no auth
// session anywhere in the flow.
//
// Before the fix the route accepted (email, recovery code) alone, so one secret
// replaced both factors and bought a password rewrite plus a live session.
// Invariant: docs/SECURITY_INVARIANTS.md → Password recovery.
func TestRecoveryResetRefusesWithoutTheAccountPassword(t *testing.T) {
	testCases := []struct {
		name     string
		password string
	}{
		{name: "no-password", password: ""},
		{name: "wrong-password", password: "NotThePassword9"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "recovery-needs-password-"+testCase.name+"@example.com", "StrongPass1", true)
			recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)

			form := url.Values{
				"email":         {user.Email},
				"recovery_code": {recoveryCode},
			}
			if testCase.password != "" {
				form.Set("password", testCase.password)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("forgot-password request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName); resetCookie != nil && strings.TrimSpace(resetCookie.Value) != "" {
				t.Fatalf("recovery reset minted a reset token without the account password")
			}
			if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
				t.Fatalf("recovery reset issued an auth session without the account password")
			}
			if response.StatusCode == http.StatusSeeOther && response.Header.Get("Location") == "/reset-password" {
				t.Fatalf("recovery reset advanced to /reset-password without the account password")
			}
		})
	}
}

// TestRecoveryResetIssuesAResetTokenForPasswordAndRecoveryCode is the positive
// control for the guard above: the legitimate owner — the SECRET_KEY-rotation
// victim who still knows the password and whose TOTP secret stopped decrypting —
// must still get through with both factors.
func TestRecoveryResetIssuesAResetTokenForPasswordAndRecoveryCode(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "recovery-with-password@example.com", "StrongPass1", true)
	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets", strings.NewReader(url.Values{
		"email":         {user.Email},
		"recovery_code": {recoveryCode},
		"password":      {"StrongPass1"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("forgot-password request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected redirect /reset-password, got %q", location)
	}
	resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatalf("expected a reset-password cookie for password + recovery code")
	}
}

// TestRecoveryResetKeepsWrongPasswordIndistinguishableFromWrongRecoveryCode pins
// the enumeration safety of the new operand. The password join must not become
// an account/credential oracle: a wrong password, a wrong recovery code, an
// account with local auth disabled and an address that has no account must all
// answer with the identical status and the identical body.
//
// Invariant: docs/SECURITY_INVARIANTS.md → Password recovery.
func TestRecoveryResetKeepsWrongPasswordIndistinguishableFromWrongRecoveryCode(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "recovery-oracle@example.com", "StrongPass1", true)
	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)

	oidcOnly := createOnboardingTestUser(t, database, "recovery-oracle-oidc@example.com", "StrongPass1", true)
	if err := database.Table("users").Where("id = ?", oidcOnly.ID).Update("local_auth_enabled", false).Error; err != nil {
		t.Fatalf("disable local auth: %v", err)
	}
	oidcOnlyRecoveryCode := mustSetRecoveryCodeForUser(t, database, oidcOnly.ID)

	probes := []struct {
		name         string
		email        string
		recoveryCode string
		password     string
	}{
		{name: "wrong-password", email: user.Email, recoveryCode: recoveryCode, password: "NotThePassword9"},
		{name: "wrong-recovery-code", email: user.Email, recoveryCode: "OVUM-ABCD-2345-EFGH", password: "StrongPass1"},
		{name: "both-wrong", email: user.Email, recoveryCode: "OVUM-ABCD-2345-EFGH", password: "NotThePassword9"},
		{name: "unknown-email", email: "recovery-oracle-absent@example.com", recoveryCode: recoveryCode, password: "StrongPass1"},
		{name: "local-auth-disabled", email: oidcOnly.Email, recoveryCode: oidcOnlyRecoveryCode, password: "StrongPass1"},
	}

	type observation struct {
		status int
		body   string
	}
	observations := make(map[string]observation, len(probes))

	// Five probes stay under the 8-failure attempt budget, so no probe can be
	// answered with a 429 and fake the agreement asserted below.
	for _, probe := range probes {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets", strings.NewReader(url.Values{
			"email":         {probe.email},
			"recovery_code": {probe.recoveryCode},
			"password":      {probe.password},
		}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("%s: forgot-password request failed: %v", probe.name, err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatalf("%s: read body: %v", probe.name, err)
		}
		if resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName); resetCookie != nil && strings.TrimSpace(resetCookie.Value) != "" {
			t.Fatalf("%s: a failing recovery probe minted a reset token", probe.name)
		}
		observations[probe.name] = observation{status: response.StatusCode, body: string(body)}
	}

	reference := observations["wrong-recovery-code"]
	if reference.status == 0 {
		t.Fatalf("missing reference observation")
	}
	for _, probe := range probes {
		got := observations[probe.name]
		if got.status != reference.status || got.body != reference.body {
			t.Fatalf("probe %q is distinguishable from a wrong recovery code: got (%d, %q), want (%d, %q)",
				probe.name, got.status, got.body, reference.status, reference.body)
		}
	}
}
