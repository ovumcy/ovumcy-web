package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterJSONContentTypeWithoutAcceptReturnsJSONError(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	payload, err := json.Marshal(map[string]any{
		"email":            "transport-register@example.com",
		"password":         "12345678",
		"confirm_password": "12345678",
		"consent":          "true",
	})
	if err != nil {
		t.Fatalf("marshal register payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "weak password" {
		t.Fatalf("expected weak password error, got %q", got)
	}
}

func TestSettingsCycleJSONContentTypeWithoutAcceptReturnsJSONError(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "transport-settings-cycle@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", bytes.NewBufferString(`{"cycle_length":`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings cycle request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid settings input" {
		t.Fatalf("expected invalid settings input error, got %q", got)
	}
}
