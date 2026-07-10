package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// TestUpdateTrackingSettingsJSONIncludesWeekStart pins the JSON response arm of
// UpdateTrackingSettings: a JSON-accepting client that saves week_starts_on gets
// the normalized value echoed back (issue #225).
func TestUpdateTrackingSettingsJSONIncludesWeekStart(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-week-start-json@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/tracking", url.Values{
		"temperature_unit": {"c"},
		"week_starts_on":   {"monday"},
	}, map[string]string{
		"Accept": "application/json",
	})
	assertStatusCode(t, response, http.StatusOK)

	var payload struct {
		WeekStartsOn string `json:"week_starts_on"`
	}
	if err := json.Unmarshal([]byte(mustReadBodyString(t, response.Body)), &payload); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
	if payload.WeekStartsOn != "monday" {
		t.Fatalf("expected week_starts_on=monday in JSON response, got %q", payload.WeekStartsOn)
	}
}
