package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func interfaceSaveForm(language string) url.Values {
	return url.Values{"language": {language}, "theme": {"dark"}}
}

func htmlAncestorWithID(node *html.Node, id string) bool {
	for current := node; current != nil; current = current.Parent {
		if current.Type == html.ElementNode && htmlAttr(current, "id") == id {
			return true
		}
	}
	return false
}

// An interface save that keeps the language answers the HTMX call in place:
// a success status for the card's own island, and no redirect or refresh that
// would reload the page to its top.
func TestSettingsInterfaceSaveWithUnchangedLanguageAnswersInPlace(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-inplace@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/interface",
		interfaceSaveForm("en"), map[string]string{"HX-Request": "true", "Accept-Language": "en"})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)
	if got := response.Header.Get("HX-Redirect"); got != "" {
		t.Fatalf("an unchanged-language save must not redirect, got HX-Redirect %q", got)
	}
	if got := response.Header.Get("HX-Refresh"); got != "" {
		t.Fatalf("an unchanged-language save must not refresh, got HX-Refresh %q", got)
	}
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlElementByTagAndClass(document, "div", "status-ok") == nil {
		t.Fatal("expected the in-place success status for the interface save")
	}
}

// A language change needs a reload; it lands on the card, not the page top, and
// when the browser is already at that URL a refresh stands in for the redirect
// (a hash-only redirect to the current URL would not reload anything).
func TestSettingsInterfaceSaveWithChangedLanguageReloadsAtTheCard(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-reload@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/interface",
		interfaceSaveForm("de"), map[string]string{"HX-Request": "true", "Accept-Language": "en"})
	defer func() { _ = response.Body.Close() }()
	assertStatusCode(t, response, http.StatusOK)
	if got := response.Header.Get("HX-Redirect"); got != "/settings#settings-interface" {
		t.Fatalf("expected HX-Redirect to the Interface card, got %q", got)
	}
	if responseCookieValue(response.Cookies(), flashCookieName) == "" {
		t.Fatal("expected the confirmation to travel in the sealed flash cookie")
	}

	atCard := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/interface",
		interfaceSaveForm("fr"), map[string]string{
			"HX-Request": "true", "Accept-Language": "en",
			"HX-Current-URL": "http://localhost/settings#settings-interface",
		})
	defer func() { _ = atCard.Body.Close() }()
	if got := atCard.Header.Get("HX-Refresh"); got != "true" {
		t.Fatalf("expected HX-Refresh when already at the card URL, got %q", got)
	}
	if got := atCard.Header.Get("HX-Redirect"); got != "" {
		t.Fatalf("a hash-only redirect to the current URL would not reload, got %q", got)
	}
}

// After the reload the confirmation sits in the Interface card's live region,
// is the focus target, and the page-top banner does not repeat it. A page with
// no flash has no focus target.
func TestSettingsPageRendersTheInterfaceFlashInsideTheCard(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-flash@example.com")

	save := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/interface",
		interfaceSaveForm("de"), nil)
	defer func() { _ = save.Body.Close() }()
	flashValue := responseCookieValue(save.Cookies(), flashCookieName)
	languageCookie := responseCookie(save.Cookies(), languageCookieName)
	if flashValue == "" || languageCookie == nil {
		t.Fatal("expected a flash cookie and a language cookie from the save")
	}

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(languageCookie), flashCookieName+"="+flashValue))
	page := mustAppResponse(t, ctx.app, request)
	assertStatusCode(t, page, http.StatusOK)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, page.Body))

	flashes := htmlFindElements(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "data-flash-key") == "settings.success.interface_updated"
	})
	if len(flashes) != 1 {
		t.Fatalf("expected exactly one interface confirmation, got %d", len(flashes))
	}
	flash := flashes[0]
	if !htmlAncestorWithID(flash, "settings-interface-status") {
		t.Fatal("the confirmation must sit in the Interface card's status island")
	}
	if htmlAttr(flash, "role") != "status" || !htmlHasAttr(flash, "data-settings-flash-focus") || htmlAttr(flash, "tabindex") != "-1" {
		t.Fatalf("the confirmation must be a focusable status node, got role=%q", htmlAttr(flash, "role"))
	}
	island := htmlElementByID(document, "settings-interface-status")
	if island == nil || htmlAttr(island, "aria-live") != "polite" {
		t.Fatal("the Interface status island must be an aria-live region")
	}

	plain := settingsFormRequestWithCSRF(t, ctx, http.MethodGet, "/settings", nil, nil)
	defer func() { _ = plain.Body.Close() }()
	plainDocument := mustParseHTMLDocument(t, mustReadBodyString(t, plain.Body))
	if htmlFindElement(plainDocument, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-settings-flash-focus")
	}) != nil {
		t.Fatal("a page load without a flash must not carry a focus target")
	}
}

// Page-top banners are live regions too.
func TestSettingsPageTopBannersAreLiveRegions(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-banner-live@example.com")

	save := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/reminders",
		url.Values{"reminder_lead_days": {"4"}}, nil)
	defer func() { _ = save.Body.Close() }()
	flashValue := responseCookieValue(save.Cookies(), flashCookieName)

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, flashCookieName+"="+flashValue))
	page := mustAppResponse(t, ctx.app, request)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, page.Body))
	banner := htmlFlashByKey(document, "settings.success.reminders_updated")
	if banner == nil {
		t.Fatal("expected the reminders confirmation banner")
	}
	if htmlAttr(banner, "role") != "status" || htmlAttr(banner, "aria-live") != "polite" {
		t.Fatalf("the page-top success banner must be a live region, got role=%q aria-live=%q", htmlAttr(banner, "role"), htmlAttr(banner, "aria-live"))
	}
}

// The theme lives in localStorage only: the server must not pre-check a theme
// the client then contradicts, and the control says where the choice is kept.
func TestSettingsInterfaceThemeIsNotServerCheckedLight(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-theme@example.com")

	page := settingsFormRequestWithCSRF(t, ctx, http.MethodGet, "/settings", nil, map[string]string{"Accept-Language": "en"})
	defer func() { _ = page.Body.Close() }()
	body := mustReadBodyString(t, page.Body)
	document := mustParseHTMLDocument(t, body)

	checked := htmlFindElements(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "input" && htmlAttr(node, "name") == "theme" && htmlHasAttr(node, "checked")
	})
	if len(checked) != 1 || htmlAttr(checked[0], "value") != "system" {
		t.Fatalf("expected the no-JS theme default to be system, got %d checked inputs", len(checked))
	}
	if !strings.Contains(body, "Saved only in this browser.") {
		t.Fatal("expected the theme control to say it is remembered in this browser only")
	}
}

// An empty, non-numeric or out-of-range lead-days value is refused with its own
// copy in the reminders form's status island, and nothing is stored.
func TestSettingsRemindersRefuseInvalidLeadDaysWithTheirOwnCopy(t *testing.T) {
	cases := []struct{ name, sent string }{
		{"empty", ""},
		{"not a number", "abc"},
		{"above max", "40"},
		{"negative", "-3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newSettingsSecurityTestContext(t, "settings-reminders-refuse-"+strings.ReplaceAll(tc.name, " ", "-")+"@example.com")

			response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/reminders",
				url.Values{"reminder_lead_days": {tc.sent}}, map[string]string{"HX-Request": "true", "Accept-Language": "en"})
			defer func() { _ = response.Body.Close() }()

			assertStatusCode(t, response, http.StatusOK)
			document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
			if htmlFlashByKey(document, "settings.error.invalid_reminder_lead_days") == nil {
				t.Fatal("expected the dedicated lead-days refusal key in the status fragment")
			}
			if htmlFlashByKey(document, "settings.error.invalid_input") != nil {
				t.Fatal("the password sentence must not be shown for a lead-days refusal")
			}

			persisted := models.User{}
			if err := ctx.database.Select("reminder_lead_days").First(&persisted, ctx.user.ID).Error; err != nil {
				t.Fatalf("load persisted reminder_lead_days: %v", err)
			}
			if persisted.ReminderLeadDays != 3 {
				t.Fatalf("a refused value must leave the stored 3 untouched, got %d", persisted.ReminderLeadDays)
			}
		})
	}
}

// The JSON caller gets the refusal as a 400 where it used to get a 200 with a
// silently clamped value.
func TestSettingsRemindersJSONRefusesOutOfRange(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-reminders-json-refuse@example.com")

	response := remindersJSONRequestWithCSRF(t, ctx, `{"reminder_lead_days": 40}`, map[string]string{"Accept": "application/json"})
	defer func() { _ = response.Body.Close() }()
	assertStatusCode(t, response, http.StatusBadRequest)
}
