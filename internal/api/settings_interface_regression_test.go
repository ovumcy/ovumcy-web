package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestSettingsInterfaceUpdateSetsLanguageCookieAndLocalizedFlash(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/interface", url.Values{
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

	// Assert the stable hooks, not localized copy: the ovumcy_lang=de cookie must
	// drive a German-rendered page (<html lang="de">, the first lang attribute in
	// the document), and the success flash surfaces via its data-flash-key.
	document := mustParseHTMLDocument(t, rendered)
	if htmlElementByAttr(document, "lang", "de") == nil {
		t.Fatalf("expected the settings page to render in German (lang=de) after the interface update, got %q", rendered)
	}
	if htmlFlashByKey(document, "settings.success.interface_updated") == nil {
		t.Fatalf("expected the interface-updated success flash key on the settings page, got %q", rendered)
	}
}

func TestSettingsInterfaceUpdateJSONReturnsNormalizedSelection(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-json@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/interface", url.Values{
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

// TestSettingsInterfaceUpdatePersistsTheLanguageOnTheAccount pins the
// account-side half of the interface save (migration 034): the language reaches
// users.interface_language, normalized to the shipped locale code, so a device
// that has no cookie is served it at the next sign-in. The theme has no
// account-side half — it stays client-side — so the same save must leave the
// rest of the row alone, which the untouched tracking column anchors.
func TestSettingsInterfaceUpdatePersistsTheLanguageOnTheAccount(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-persist@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/interface", url.Values{
		"language": {"ru-RU"},
		"theme":    {"dark"},
	}, nil)
	assertStatusCode(t, response, http.StatusSeeOther)

	var stored models.User
	if err := ctx.database.First(&stored, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user after the interface save: %v", err)
	}
	if stored.InterfaceLanguage != "ru" {
		t.Fatalf("expected users.interface_language=ru after the save, got %q", stored.InterfaceLanguage)
	}
	if stored.TemperatureUnit != "c" {
		t.Fatalf("expected the interface save to touch no other column, temperature_unit=%q", stored.TemperatureUnit)
	}
}

// TestSettingsInterfaceUpdateReportsAFailedAccountWrite proves the save is not
// silently degraded to a cookie-only change when the account write fails: the
// owner is told, and the language cookie is NOT issued, so the two stores
// cannot part company behind a success flash. The failure is injected by
// removing the column the save targets, which leaves every earlier step of the
// request (session, CSRF, validation) working.
func TestSettingsInterfaceUpdateReportsAFailedAccountWrite(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-write-fails@example.com")

	if err := ctx.database.Exec("ALTER TABLE users DROP COLUMN interface_language").Error; err != nil {
		t.Fatalf("drop interface_language column: %v", err)
	}

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/interface", url.Values{
		"language": {"ru"},
		"theme":    {"dark"},
	}, map[string]string{
		"Accept": "application/json",
	})
	assertStatusCode(t, response, http.StatusInternalServerError)

	if got := readAPIError(t, response.Body); got != "failed to update interface settings" {
		t.Fatalf("expected the interface-update failure envelope, got %q", got)
	}
	if cookie := responseCookie(response.Cookies(), languageCookieName); cookie != nil {
		t.Fatalf("expected no language cookie when the account write failed, got %#v", cookie)
	}
}

func TestSettingsInterfaceUpdateRejectsInvalidTheme(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-invalid@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/interface", url.Values{
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
