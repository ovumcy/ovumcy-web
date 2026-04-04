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

func TestSettingsTrackingSectionExplainsToggleEffectsAndCurrentState(t *testing.T) {
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
	if !strings.Contains(htmlDocumentText(document), "Existing values stay in your private history and exports.") {
		t.Fatalf("expected tracking subtitle to explain saved-value behavior")
	}

	testCases := []struct {
		attribute string
		hint      string
		state     string
	}{
		{
			attribute: "track-bbt",
			hint:      "Adds a basal body temperature field to dashboard and calendar day editing.",
			state:     "Currently hidden from new dashboard and calendar entries.",
		},
		{
			attribute: "track-cervical-mucus",
			hint:      "Adds cervical mucus choices to dashboard and calendar day editing.",
			state:     "Currently hidden from new dashboard and calendar entries.",
		},
		{
			attribute: "hide-sex-chip",
			hint:      "Removes the intimacy section from new dashboard and calendar entries.",
			state:     "Currently visible in dashboard and calendar day editor.",
		},
	}

	for _, tc := range testCases {
		toggle := htmlFindElement(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "data-tracking-setting") == tc.attribute
		})
		if toggle == nil {
			t.Fatalf("expected tracking toggle %q", tc.attribute)
		}

		toggleText := normalizeHTMLText(htmlNodeText(toggle))
		if !strings.Contains(toggleText, tc.hint) {
			t.Fatalf("expected tracking toggle %q to include hint %q, got %q", tc.attribute, tc.hint, toggleText)
		}
		if !strings.Contains(toggleText, tc.state) {
			t.Fatalf("expected tracking toggle %q to include state %q, got %q", tc.attribute, tc.state, toggleText)
		}

		state := htmlFindElement(toggle, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlHasAttr(node, "data-binary-toggle-state")
		})
		if state == nil {
			t.Fatalf("expected tracking toggle %q to render live state node", tc.attribute)
		}
		if htmlAttr(state, "data-state-on") == "" || htmlAttr(state, "data-state-off") == "" {
			t.Fatalf("expected tracking toggle %q to provide live state labels", tc.attribute)
		}
	}
}

func TestSettingsInterfaceSectionRendersDraftSaveContract(t *testing.T) {
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

	rendered := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `data-settings-interface-form`, message: "expected interface settings form"},
		bodyStringMatch{fragment: `action="/api/settings/interface"`, message: "expected interface form action"},
		bodyStringMatch{fragment: `data-settings-interface-save`, message: "expected interface save control"},
		bodyStringMatch{fragment: `data-settings-interface-discard`, message: "expected interface discard control"},
		bodyStringMatch{fragment: `data-settings-interface-language-option="en"`, message: "expected interface language radio tiles"},
		bodyStringMatch{fragment: `data-settings-interface-theme-option="dark"`, message: "expected interface theme radio tiles"},
		bodyStringMatch{fragment: "Theme preview updates immediately and is saved in this browser when you press Save.", message: "expected interface preview hint"},
	)
	assertBodyNotContainsAll(t, rendered,
		bodyStringMatch{fragment: `class="lang-link`, message: "did not expect legacy language links in settings interface"},
	)
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
