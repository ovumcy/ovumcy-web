package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSettingsInterfaceUpdateSetsLanguageCookieAndLocalizedFlash(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/interface", url.Values{
		"language": {"de-DE"},
		"theme":    {"dark"},
	}, nil)
	assertStatusCode(t, response, http.StatusSeeOther)

	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", location)
	}

	languageCookie := responseCookie(response.Cookies(), languageCookieName)
	if languageCookie == nil || languageCookie.Value != "de" {
		t.Fatalf("expected ovumcy_lang cookie=de, got %#v", languageCookie)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatal("expected flash cookie for interface update")
	}

	followRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	followRequest.Header.Set("Accept-Language", "en")
	followRequest.Header.Set("Cookie", joinCookieHeader(
		ctx.authCookie,
		cookiePair(languageCookie),
		flashCookieName+"="+flashValue,
	))

	followResponse := mustAppResponse(t, ctx.app, followRequest)
	assertStatusCode(t, followResponse, http.StatusOK)
	rendered := mustReadBodyString(t, followResponse.Body)

	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: "Einstellungen", message: "expected settings page in German after interface update"},
		bodyStringMatch{fragment: "Oberflächeneinstellungen aktualisiert.", message: "expected localized interface success flash"},
	)
}

func TestSettingsInterfaceUpdateJSONReturnsNormalizedSelection(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-json@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/interface", url.Values{
		"language": {"fr-FR"},
		"theme":    {"dark"},
	}, map[string]string{
		"Accept": "application/json",
	})
	assertStatusCode(t, response, http.StatusOK)

	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode interface settings json response: %v", err)
	}

	if got, ok := payload["ok"].(bool); !ok || !got {
		t.Fatalf("expected ok=true payload, got %#v", payload)
	}
	if got := stringValue(payload["status"]); got != "interface_updated" {
		t.Fatalf("expected status interface_updated, got %#v", payload["status"])
	}
	if got := stringValue(payload["language"]); got != "fr" {
		t.Fatalf("expected normalized language fr, got %#v", payload["language"])
	}
	if got := stringValue(payload["theme"]); got != "dark" {
		t.Fatalf("expected normalized theme dark, got %#v", payload["theme"])
	}

	if languageCookie := responseCookieValue(response.Cookies(), languageCookieName); languageCookie != "fr" {
		t.Fatalf("expected language cookie fr, got %q", languageCookie)
	}
}

func TestSettingsInterfaceUpdateRejectsInvalidTheme(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-invalid@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/interface", url.Values{
		"language": {"en"},
		"theme":    {"sepia"},
	}, map[string]string{
		"Accept": "application/json",
	})
	assertStatusCode(t, response, http.StatusBadRequest)

	if got := readAPIError(t, response.Body); got != "invalid settings input" {
		t.Fatalf("expected invalid settings input error, got %q", got)
	}
}
