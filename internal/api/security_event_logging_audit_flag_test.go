package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestAuditLogDefaultOffSuppressesSecurityEvents asserts the production
// default: with AUDIT_LOG_ENABLED unset (or false), exercising a fully
// audited endpoint produces no `security event:` line on stderr. This is
// the contract documented in SECURITY.md under "Logging Policy".
func TestAuditLogDefaultOffSuppressesSecurityEvents(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "audit-default-off@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-17", strings.NewReader(url.Values{
		"is_period": {"true"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("day upsert request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	if strings.Contains(output.String(), "security event:") {
		t.Fatalf("expected no security-event output with AUDIT_LOG_ENABLED=false, got %q", output.String())
	}
}

// TestAuditLogEnabledRestoresSecurityEvents confirms the flag is honored
// in the opposite direction: turning it on re-enables the audit stream.
func TestAuditLogEnabledRestoresSecurityEvents(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{auditLogEnabled: true})
	user := createOnboardingTestUser(t, database, "audit-enabled@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-17", strings.NewReader(url.Values{
		"is_period": {"true"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("day upsert request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	if !strings.Contains(output.String(), `security event: action="health.day_upsert"`) {
		t.Fatalf("expected security-event output with AUDIT_LOG_ENABLED=true, got %q", output.String())
	}
}
