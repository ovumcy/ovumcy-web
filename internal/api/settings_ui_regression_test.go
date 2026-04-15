package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestSettingsPageRendersSingleIrregularCycleExplanation(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-irregular-copy@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	documentText := htmlDocumentText(mustParseHTMLDocument(t, rendered))
	const hint = "Turn this on if your cycles vary by more than 7 days. Ranges will be used for next period and ovulation instead of a single date."
	if strings.Count(documentText, hint) != 1 {
		t.Fatalf("expected a single irregular-cycle explanation, got %q", documentText)
	}
}

func TestSettingsPageUsesMedicalSectionsBeforeInterfaceAndDangerZone(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-section-order@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	order := []string{
		"settings-cycle",
		"settings-symptoms-section",
		"settings-tracking",
		"settings-interface",
		"settings-account",
		"settings-data",
		"settings-danger-zone",
	}

	sectionIDs := htmlSectionIDs(document)
	lastIndex := -1
	for _, expectedID := range order {
		currentIndex := slices.Index(sectionIDs, expectedID)
		if currentIndex == -1 {
			t.Fatalf("expected settings page to contain %q", expectedID)
		}
		if currentIndex <= lastIndex {
			t.Fatalf("expected settings section %q after previous sections", expectedID)
		}
		lastIndex = currentIndex
	}
	if slices.Contains(sectionIDs, "settings-reminders") {
		t.Fatalf("did not expect deprecated reminders section, got %v", sectionIDs)
	}
}

func TestSettingsPageExplainsClearDataRemovesCustomSymptoms(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-clear-data-copy@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	documentText := htmlDocumentText(mustParseHTMLDocument(t, mustReadBodyString(t, response.Body)))
	if !strings.Contains(documentText, "remove your custom symptoms") {
		t.Fatalf("expected clear-data copy to explain custom symptom removal, got %q", documentText)
	}
}

func TestSettingsTrackingSectionRendersExpectedToggleContracts(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-copy@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	if strings.Contains(rendered, "settings.tracking.cervical_mucus_explainer") {
		t.Fatalf("expected tracking section to use translated helper copy instead of a missing explainer key")
	}

	document := mustParseHTMLDocument(t, rendered)
	trackingSection := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "id") == "settings-tracking"
	})
	if trackingSection == nil {
		t.Fatal("expected settings tracking section")
	}

	expectedToggles := []string{
		"track-bbt",
		"track-cervical-mucus",
		"hide-sex-chip",
		"hide-cycle-factors",
		"hide-notes-field",
	}

	for _, attribute := range expectedToggles {
		toggle := htmlFindElement(trackingSection, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "data-tracking-setting") == attribute
		})
		if toggle == nil {
			t.Fatalf("expected tracking toggle %q", attribute)
		}

		toggleText := normalizeHTMLText(htmlNodeText(toggle))
		if toggleText == "" {
			t.Fatalf("expected tracking toggle %q to render non-empty user-facing copy", attribute)
		}
	}
}

func TestSettingsDataSectionExplainsServerStorageAndUIOnlyHiding(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-data-copy@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	documentText := htmlDocumentText(mustParseHTMLDocument(t, mustReadBodyString(t, response.Body)))
	if !strings.Contains(documentText, "SQLite or PostgreSQL database on this server") {
		t.Fatalf("expected settings data section to describe server-side storage, got %q", documentText)
	}
	if !strings.Contains(documentText, "Hidden entry sections are a UI-only preference.") {
		t.Fatalf("expected settings data section to explain hidden-section export behavior, got %q", documentText)
	}
}

func TestSettingsInterfaceSectionRendersSaveDiscardContract(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-ui@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	form := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-settings-interface-form")
	})
	if form == nil {
		t.Fatal("expected interface settings form")
	}
	if got := htmlAttr(form, "action"); got != "/api/settings/interface" {
		t.Fatalf("expected interface form action /api/settings/interface, got %q", got)
	}
	if htmlFindElement(form, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-settings-interface-save")
	}) == nil {
		t.Fatal("expected interface save control")
	}
	if htmlFindElement(form, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-settings-interface-discard")
	}) == nil {
		t.Fatal("expected interface discard control")
	}
	if htmlFindElement(form, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "data-settings-interface-language-option") == "en"
	}) == nil {
		t.Fatal("expected English language option in interface form")
	}
	if htmlFindElement(form, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "data-settings-interface-theme-option") == "dark"
	}) == nil {
		t.Fatal("expected dark theme option in interface form")
	}
}

func TestSettingsDangerZoneDeleteAccountCardShowsVisibleTitle(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-danger-title@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	deleteCard := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasClass(node, "danger-card-soft") && htmlFindElement(node, func(child *html.Node) bool {
			return child.Type == html.ElementNode && htmlAttr(child, "hx-delete") == "/api/settings/delete-account"
		}) != nil
	})
	if deleteCard == nil {
		t.Fatal("expected delete-account danger card")
	}

	deleteCardText := normalizeHTMLText(htmlNodeText(deleteCard))
	if !strings.Contains(deleteCardText, "Delete account") {
		t.Fatalf("expected delete-account danger card to include a visible title, got %q", deleteCardText)
	}
}

func TestSettingsCycleAndTrackingSectionsRenderDraftDiscardContract(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-cycle-tracking-draft-ui@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `data-settings-draft-form="cycle"`, message: "expected cycle draft form contract"},
		bodyStringMatch{fragment: `data-settings-cycle-save`, message: "expected cycle save hook"},
		bodyStringMatch{fragment: `data-settings-cycle-discard`, message: "expected cycle discard control"},
		bodyStringMatch{fragment: `data-settings-draft-form="tracking"`, message: "expected tracking draft form contract"},
		bodyStringMatch{fragment: `data-settings-tracking-save`, message: "expected tracking save hook"},
		bodyStringMatch{fragment: `data-settings-tracking-discard`, message: "expected tracking discard control"},
		bodyStringMatch{fragment: "You have unsaved settings changes. Leave without saving?", message: "expected shared settings unsaved prompt"},
		bodyStringMatch{fragment: "Discard changes", message: "expected shared settings discard copy"},
	)
}

func TestForgotPasswordEmailStepUsesGenericEnumerationSafeSubtitle(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	form := url.Values{"email": {"unknown-owner@example.com"}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("forgot-password email step request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected sealed flash cookie after forgot-password email step")
	}

	followRequest := httptest.NewRequest(http.MethodGet, "/forgot-password", nil)
	followRequest.Header.Set("Accept-Language", "en")
	followRequest.Header.Set("Cookie", flashCookieName+"="+flashValue)

	followResponse, err := app.Test(followRequest, -1)
	if err != nil {
		t.Fatalf("forgot-password follow-up request failed: %v", err)
	}
	defer followResponse.Body.Close()

	if followResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected forgot-password follow-up status 200, got %d", followResponse.StatusCode)
	}

	documentText := htmlDocumentText(mustParseHTMLDocument(t, mustReadBodyString(t, followResponse.Body)))
	if !strings.Contains(documentText, "If this address is registered, enter your recovery code to continue.") {
		t.Fatalf("expected generic recovery subtitle after email step, got %q", documentText)
	}
}
