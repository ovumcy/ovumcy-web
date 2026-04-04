package api

import (
	"net/url"
	"testing"
)

func TestSettingsInterfaceUpdateUsesFlashSuccessOnRedirect(t *testing.T) {
	form := url.Values{
		"language": {"de"},
		"theme":    {"dark"},
	}
	assertSettingsFlashSuccessScenario(t, "/api/settings/interface", form, "Interface settings updated.")
}
