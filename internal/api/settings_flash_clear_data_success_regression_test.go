package api

import (
	"net/url"
	"testing"
)

func TestSettingsClearDataUsesFlashSuccessOnRedirect(t *testing.T) {
	assertSettingsFlashSuccessScenario(t, "/api/settings/clear-data", url.Values{
		"password": {"StrongPass1"},
	}, "All tracking data cleared successfully.")
}
