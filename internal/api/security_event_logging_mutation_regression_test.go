package api

import (
	"bytes"
	"log"
	"net/http"
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
