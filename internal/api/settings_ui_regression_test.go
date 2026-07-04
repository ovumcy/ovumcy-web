package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

func TestSettingsPageRendersSingleIrregularCycleHint(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-irregular-hint@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	hints := htmlFindElements(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-settings-irregular-cycle-hint")
	})
	if len(hints) != 1 {
		t.Fatalf("expected exactly one irregular-cycle hint element, got %d", len(hints))
	}
}

func TestSettingsPageUsesMedicalSectionsBeforeInterfaceAndDangerZone(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-section-order@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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

func TestSettingsTrackingSectionRendersExpectedToggleContracts(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-copy@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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

// TestSettingsTrackingTogglesReflectPersistedState pins that every tracking
// toggle — including show-historical-phases, which was missing from the
// settings render map so the toggle always rendered OFF regardless of the
// saved value — reflects the persisted user setting on initial page load.
func TestSettingsTrackingTogglesReflectPersistedState(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-state@example.com")

	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Updates(map[string]any{
		"track_bbt":              true,
		"track_cervical_mucus":   true,
		"hide_sex_chip":          true,
		"hide_cycle_factors":     true,
		"hide_notes_field":       true,
		"show_historical_phases": true,
	}).Error; err != nil {
		t.Fatalf("persist tracking settings: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)
	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	for _, toggle := range []string{
		"track-bbt", "track-cervical-mucus", "hide-sex-chip",
		"hide-cycle-factors", "hide-notes-field", "show-historical-phases",
	} {
		node := htmlFindElement(document, func(n *html.Node) bool {
			return n.Type == html.ElementNode && htmlAttr(n, "data-tracking-setting") == toggle
		})
		if node == nil {
			t.Fatalf("expected tracking toggle %q", toggle)
		}
		if htmlAttr(node, "data-active") != "true" {
			t.Errorf("toggle %q must render data-active=true for a persisted enabled setting, got %q", toggle, htmlAttr(node, "data-active"))
		}
	}
}

func TestSettingsInterfaceSectionRendersSaveDiscardContract(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-interface-ui@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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
	if got := htmlAttr(form, "action"); got != "/api/v1/users/current/interface" {
		t.Fatalf("expected interface form action /api/v1/users/current/interface, got %q", got)
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

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	deleteCard := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasClass(node, "danger-card-soft") && htmlFindElement(node, func(child *html.Node) bool {
			return child.Type == html.ElementNode && htmlAttr(child, "hx-delete") == "/api/v1/users/current"
		}) != nil
	})
	if deleteCard == nil {
		t.Fatal("expected delete-account danger card")
	}

	titleElement := htmlFindElement(deleteCard, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasClass(node, "field-label")
	})
	if titleElement == nil {
		t.Fatal("expected delete-account danger card to include a visible field-label title element")
	}
	if normalizeHTMLText(htmlNodeText(titleElement)) == "" {
		t.Fatal("expected delete-account danger card title to render non-empty user-facing copy")
	}
}

func TestSettingsCycleAndTrackingSectionsRenderDraftDiscardContract(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-cycle-tracking-draft-ui@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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
		bodyStringMatch{fragment: `data-settings-unsaved-prompt=`, message: "expected shared unsaved-prompt hook on draft forms"},
		bodyStringMatch{fragment: `data-settings-unsaved-accept=`, message: "expected shared unsaved-accept hook on draft forms"},
	)
}

func TestForgotPasswordEmailStepUsesGenericEnumerationSafeSubtitle(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	form := url.Values{"email": {"unknown-owner@example.com"}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("forgot-password email step request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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

	followResponse, err := app.Test(followRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("forgot-password follow-up request failed: %v", err)
	}
	defer func() { _ = followResponse.Body.Close() }()

	if followResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected forgot-password follow-up status 200, got %d", followResponse.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, followResponse.Body))
	subtitle := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-subtitle-key")
	})
	if subtitle == nil {
		t.Fatal("expected forgot-password subtitle element to expose data-subtitle-key")
	}
	if got := htmlAttr(subtitle, "data-subtitle-key"); got != "auth.forgot_password_step2_subtitle" {
		t.Fatalf("expected enumeration-safe recovery-step subtitle key, got %q", got)
	}
	if got := htmlAttr(subtitle, "data-forgot-step"); got != "recovery_code" {
		t.Fatalf("expected forgot-step %q, got %q", "recovery_code", got)
	}
}
