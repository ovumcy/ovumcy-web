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
		"settings-symptoms",
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

func TestSettingsSectionNavIndexesEverySection(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-section-nav@example.com")

	document := mustParseHTMLDocument(t, renderSettingsPageForTest(t, ctx.app, ctx.authCookie))
	nav := htmlElementByTagAndClass(document, "nav", "settings-section-nav")
	if nav == nil {
		t.Fatal("expected the settings section navigation")
	}

	targets := make([]string, 0, 10)
	for _, link := range htmlFindElements(nav, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a"
	}) {
		targets = append(targets, strings.TrimPrefix(htmlAttr(link, "href"), "#"))
	}

	// The page's own sections are the direct children of its root; the nested
	// ones (change-password inside the account card) are not separate
	// destinations and carry no link.
	//
	// Recognised by the data-settings-section marker rather than by a tag name:
	// these cards are <details> disclosures, and keying on the literal "section"
	// made this guard report an empty list — every link "extra", the index
	// apparently indexing nothing — the day the tag changed.
	sectionIDs := make([]string, 0, 10)
	for child := nav.Parent.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || !htmlHasAttr(child, "data-settings-section") {
			continue
		}
		if id := htmlAttr(child, "id"); id != "" {
			sectionIDs = append(sectionIDs, id)
		}
	}

	if !slices.Equal(targets, sectionIDs) {
		t.Fatalf("expected the section index to list every rendered section in order, got links %v for sections %v", targets, sectionIDs)
	}
	if !slices.Contains(targets, "settings-danger-zone") {
		t.Fatal("expected the destructive section to stay reachable from the section index")
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
		"show-sex-chip",
		"show-cycle-factors",
		"show-notes-field",
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
//
// The three section toggles are the polarity case: their columns are stored
// inverted (hide_sex_chip, hide_cycle_factors, hide_notes_field) while the row
// is labelled and rendered as "show", so this asserts the rendered state
// against the stored column in both directions. A second conversion on the
// render path flips one of these pairs and nothing else notices.
func TestSettingsTrackingTogglesReflectPersistedState(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-state@example.com")

	for _, testCase := range []struct {
		name    string
		stored  map[string]any
		expects map[string]bool
	}{
		{
			name: "every stored column set",
			stored: map[string]any{
				"track_bbt":              true,
				"track_cervical_mucus":   true,
				"hide_sex_chip":          true,
				"hide_cycle_factors":     true,
				"hide_notes_field":       true,
				"show_historical_phases": true,
			},
			expects: map[string]bool{
				"track-bbt":              true,
				"track-cervical-mucus":   true,
				"show-sex-chip":          false,
				"show-cycle-factors":     false,
				"show-notes-field":       false,
				"show-historical-phases": true,
			},
		},
		{
			name: "every stored column clear",
			stored: map[string]any{
				"track_bbt":              false,
				"track_cervical_mucus":   false,
				"hide_sex_chip":          false,
				"hide_cycle_factors":     false,
				"hide_notes_field":       false,
				"show_historical_phases": false,
			},
			expects: map[string]bool{
				"track-bbt":              false,
				"track-cervical-mucus":   false,
				"show-sex-chip":          true,
				"show-cycle-factors":     true,
				"show-notes-field":       true,
				"show-historical-phases": false,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Updates(testCase.stored).Error; err != nil {
				t.Fatalf("persist tracking settings: %v", err)
			}

			document := renderSettingsDocumentForTest(t, ctx)
			for toggle, expected := range testCase.expects {
				assertTrackingToggleState(t, document, toggle, expected)
			}
		})
	}
}

// TestSettingsTrackingSaveRoundTripsTheSectionTogglesPositively drives the
// whole loop the single conversion point has to keep honest: the settings form
// posts "shown", persistence stores "hidden", and the page renders the checkbox
// back out of that stored column. An unchecked box posts nothing, which is what
// makes the polarity load-bearing — omitting show_notes_field must store a
// hidden notes section, and re-rendering it must not flip back.
func TestSettingsTrackingSaveRoundTripsTheSectionTogglesPositively(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-polarity@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/tracking", url.Values{
		"show_sex_chip":      {"true"},
		"show_cycle_factors": {"true"},
		"temperature_unit":   {"c"},
	}, map[string]string{"HX-Request": "true"})
	assertStatusCode(t, response, http.StatusOK)

	var persisted struct {
		HideSexChip      bool
		HideCycleFactors bool
		HideNotesField   bool
	}
	if err := ctx.database.Model(&models.User{}).
		Select("hide_sex_chip", "hide_cycle_factors", "hide_notes_field").
		Where("id = ?", ctx.user.ID).
		First(&persisted).Error; err != nil {
		t.Fatalf("load persisted tracking columns: %v", err)
	}
	if persisted.HideSexChip || persisted.HideCycleFactors {
		t.Fatalf("a checked show_* toggle must clear its stored hide_* column, got %+v", persisted)
	}
	if !persisted.HideNotesField {
		t.Fatalf("an omitted show_notes_field must store hide_notes_field=true, got %+v", persisted)
	}

	document := renderSettingsDocumentForTest(t, ctx)
	assertTrackingToggleState(t, document, "show-sex-chip", true)
	assertTrackingToggleState(t, document, "show-cycle-factors", true)
	assertTrackingToggleState(t, document, "show-notes-field", false)
}

func renderSettingsDocumentForTest(t *testing.T, ctx settingsSecurityTestContext) *html.Node {
	t.Helper()

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
	return mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
}

// assertTrackingToggleState checks both halves of a rendered toggle — the
// data-active hook and the checkbox's own checked attribute — so a render path
// that agrees with itself only in the attribute cannot pass.
func assertTrackingToggleState(t *testing.T, document *html.Node, toggle string, active bool) {
	t.Helper()

	node := htmlFindElement(document, func(n *html.Node) bool {
		return n.Type == html.ElementNode && htmlAttr(n, "data-tracking-setting") == toggle
	})
	if node == nil {
		t.Fatalf("expected tracking toggle %q", toggle)
	}

	expected := "false"
	if active {
		expected = "true"
	}
	if got := htmlAttr(node, "data-active"); got != expected {
		t.Errorf("toggle %q must render data-active=%s for the persisted state, got %q", toggle, expected, got)
	}

	input := htmlFindElement(node, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "input" && htmlHasAttr(n, "data-binary-toggle-input")
	})
	if input == nil {
		t.Fatalf("expected tracking toggle %q to carry a checkbox", toggle)
	}
	if checked := htmlHasAttr(input, "checked"); checked != active {
		t.Errorf("toggle %q must render checked=%t for the persisted state, got %t", toggle, active, checked)
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
	// All three theme preferences are first-class options; "system" is the one
	// whose radio value the interface endpoint has to accept as well.
	for _, theme := range []string{"light", "dark", "system"} {
		option := htmlFindElement(form, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "data-settings-interface-theme-option") == theme
		})
		if option == nil {
			t.Fatalf("expected %s theme option in interface form", theme)
		}
		if htmlFindElement(option, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "input" && htmlAttr(node, "name") == "theme" && htmlAttr(node, "value") == theme
		}) == nil {
			t.Fatalf("expected %s theme option to carry a theme radio input", theme)
		}
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
		return node.Type == html.ElementNode && htmlHasClass(node, "card-danger-soft") && htmlFindElement(node, func(child *html.Node) bool {
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
