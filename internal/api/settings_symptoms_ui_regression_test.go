package api

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestSettingsSymptomsSectionExplainsEmptyAndArchivedStates(t *testing.T) {
	t.Run("no custom symptoms", func(t *testing.T) {
		ctx := newSettingsSecurityTestContext(t, "settings-symptoms-empty@example.com")

		document := mustParseHTMLDocument(t, renderSettingsPageForTest(t, ctx.app, ctx.authCookie))
		section := htmlElementByID(document, "settings-symptoms-section")
		if section == nil {
			t.Fatal("expected settings symptoms section")
		}

		sectionText := normalizeHTMLText(htmlNodeText(section))
		assertTextContains(t, sectionText, "Create short private labels for patterns you want to log.")
		assertTextContains(t, sectionText, "No custom symptoms yet. Add one above to make it available in new entries.")

		if htmlFindElement(section, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "data-symptom-group") == "active"
		}) != nil {
			t.Fatal("did not expect active custom symptom group when no custom symptoms exist")
		}
		if htmlFindElement(section, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "data-symptom-group") == "archived"
		}) != nil {
			t.Fatal("did not expect archived custom symptom group when no custom symptoms exist")
		}
		if htmlFindElement(section, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "data-symptom-empty-state") == "empty"
		}) == nil {
			t.Fatal("expected empty custom symptom state panel")
		}
	})

	t.Run("only archived custom symptoms", func(t *testing.T) {
		ctx := newSettingsSecurityTestContext(t, "settings-symptoms-archived@example.com")

		archivedAt := time.Now().UTC()
		symptom := models.SymptomType{
			UserID:     ctx.user.ID,
			Name:       "Joint relief",
			Icon:       "✨",
			Color:      "#F5A623",
			ArchivedAt: &archivedAt,
		}
		if err := ctx.database.Create(&symptom).Error; err != nil {
			t.Fatalf("create archived custom symptom: %v", err)
		}

		document := mustParseHTMLDocument(t, renderSettingsPageForTest(t, ctx.app, ctx.authCookie))
		section := htmlElementByID(document, "settings-symptoms-section")
		if section == nil {
			t.Fatal("expected settings symptoms section")
		}

		sectionText := normalizeHTMLText(htmlNodeText(section))
		assertTextContains(t, sectionText, "No visible custom symptoms right now. Restore one below or add a new one above.")
		assertTextContains(t, sectionText, "Past logs keep them. Restore one when you want it back in the picker.")

		if htmlFindElement(section, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "data-symptom-empty-state") == "active"
		}) == nil {
			t.Fatal("expected active-empty state when only archived custom symptoms remain")
		}
		archivedGroup := htmlFindElement(section, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "data-symptom-group") == "archived"
		})
		if archivedGroup == nil {
			t.Fatal("expected archived custom symptom group")
		}
		assertTextContains(t, normalizeHTMLText(htmlNodeText(archivedGroup)), "Joint relief")
		assertTextContains(t, normalizeHTMLText(htmlNodeText(archivedGroup)), "Hidden")
	})
}

func assertTextContains(t *testing.T, value string, fragment string) {
	t.Helper()
	if !strings.Contains(value, fragment) {
		t.Fatalf("expected %q to contain %q", value, fragment)
	}
}

// TestSettingsSymptomIconInCatalog pins catalog membership (survivor: the
// `option == value` equality): a known catalog glyph is a member, an arbitrary
// custom glyph is not. Membership decides whether a custom icon gets its own
// leading option in the select.
func TestSettingsSymptomIconInCatalog(t *testing.T) {
	t.Parallel()

	if !settingsSymptomIconInCatalog(settingsSymptomIconCatalog[0]) {
		t.Fatalf("expected %q to be reported in catalog", settingsSymptomIconCatalog[0])
	}
	if settingsSymptomIconInCatalog("🦄") {
		t.Fatal("expected a non-catalog glyph to be reported outside the catalog")
	}
}

// TestBuildSettingsSymptomIconOptions pins the option list built for the icon
// select. For a catalog icon: no extra custom option, and exactly the matching
// catalog entry is Selected (survivor: the `value == selected` equality). For a
// custom icon: a leading IsCustom+Selected option is prepended and no catalog
// entry is selected.
func TestBuildSettingsSymptomIconOptions(t *testing.T) {
	t.Parallel()

	t.Run("catalog icon selects exactly one entry", func(t *testing.T) {
		t.Parallel()
		current := settingsSymptomIconCatalog[2]
		options := buildSettingsSymptomIconOptions(current)
		if len(options) != len(settingsSymptomIconCatalog) {
			t.Fatalf("expected %d options for a catalog icon, got %d", len(settingsSymptomIconCatalog), len(options))
		}
		selectedCount := 0
		for _, option := range options {
			if option.IsCustom {
				t.Fatalf("did not expect a custom option for a catalog icon, got %q", option.Value)
			}
			if option.Selected {
				selectedCount++
				if option.Value != current {
					t.Fatalf("selected option = %q, want %q", option.Value, current)
				}
			}
		}
		if selectedCount != 1 {
			t.Fatalf("expected exactly one selected option, got %d", selectedCount)
		}
	})

	t.Run("custom icon prepends a selected custom option", func(t *testing.T) {
		t.Parallel()
		options := buildSettingsSymptomIconOptions("🦄")
		if len(options) != len(settingsSymptomIconCatalog)+1 {
			t.Fatalf("expected catalog+1 options for a custom icon, got %d", len(options))
		}
		if !options[0].IsCustom || !options[0].Selected || options[0].Value != "🦄" {
			t.Fatalf("expected leading custom selected option, got %#v", options[0])
		}
		for _, option := range options[1:] {
			if option.Selected {
				t.Fatalf("did not expect any catalog entry selected for a custom icon, got %q", option.Value)
			}
		}
	})
}

// TestBuildSettingsSymptomRowsUsesDraftOnlyForMatchingRow pins the draft-value
// gate (survivor: the compound `SymptomID != 0 && SymptomID == symptom.ID &&
// UseDraftValues`): the edited row shows the draft name, every other row shows
// its stored name, and a zero SymptomID never draft-fills.
func TestBuildSettingsSymptomRowsUsesDraftOnlyForMatchingRow(t *testing.T) {
	t.Parallel()

	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps", Icon: "🔥"},
		{ID: 2, Name: "Headache", Icon: "🤕"},
	}
	identity := func(s string) string { return s }

	rows := buildSettingsSymptomRows(symptoms, settingsSymptomRowState{
		SymptomID:      2,
		UseDraftValues: true,
		Draft:          symptomPayload{Name: "Migraine", Icon: "🌀"},
	}, identity, identity)

	if rows[0].FormName != "Cramps" {
		t.Fatalf("non-edited row should keep stored name, got %q", rows[0].FormName)
	}
	if rows[1].FormName != "Migraine" {
		t.Fatalf("edited row should use draft name, got %q", rows[1].FormName)
	}

	// A zero SymptomID must never draft-fill any row.
	rowsZero := buildSettingsSymptomRows(symptoms, settingsSymptomRowState{
		SymptomID:      0,
		UseDraftValues: true,
		Draft:          symptomPayload{Name: "Ghost", Icon: "🌀"},
	}, identity, identity)
	for _, row := range rowsZero {
		if row.FormName == "Ghost" {
			t.Fatalf("zero SymptomID must not draft-fill, got %q", row.FormName)
		}
	}
}
