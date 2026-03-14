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

func TestCreateSymptomLogsMutationWithoutLeakingUserInput(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	ctx := newSettingsSecurityTestContext(t, "settings-symptom-audit@example.com")
	form := url.Values{
		"name": {"=Cycle secret"},
		"icon": {"S"},
	}

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/symptoms", form, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, `security event: action="health.symptom_create" outcome="success"`) {
		t.Fatalf("expected health symptom create security event, got %q", logLine)
	}
	if !strings.Contains(logLine, `domain="health_data"`) {
		t.Fatalf("expected health_data domain in log line, got %q", logLine)
	}
	if !strings.Contains(logLine, `target="symptom"`) {
		t.Fatalf("expected symptom target in log line, got %q", logLine)
	}
	if strings.Contains(logLine, "=Cycle secret") {
		t.Fatalf("did not expect symptom name in mutation logs: %q", logLine)
	}
}

func TestUpsertDayLogsSanitizedPathWithoutConcreteDate(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-day-audit@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/days/2026-02-17", strings.NewReader(url.Values{
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

	logLine := output.String()
	if !strings.Contains(logLine, `security event: action="health.day_upsert" outcome="success"`) {
		t.Fatalf("expected health.day_upsert security event, got %q", logLine)
	}
	if !strings.Contains(logLine, `path="/api/days/:date"`) {
		t.Fatalf("expected sanitized day route in log line, got %q", logLine)
	}
	if strings.Contains(logLine, "2026-02-17") {
		t.Fatalf("did not expect concrete health date in mutation logs: %q", logLine)
	}
}
