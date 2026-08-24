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
		section := htmlElementByID(document, "settings-symptoms")
		if section == nil {
			t.Fatal("expected settings symptoms section")
		}

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
		section := htmlElementByID(document, "settings-symptoms")
		if section == nil {
			t.Fatal("expected settings symptoms section")
		}

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
	})
}

func assertTextContains(t *testing.T, value string, fragment string) {
	t.Helper()
	if !strings.Contains(value, fragment) {
		t.Fatalf("expected %q to contain %q", value, fragment)
	}
}

// The view-model unit tests that used to sit here — catalog membership, the
// icon-option list, and the draft/zero-ID row gate — are the same decisions
// settings_mutation_view_models_test.go pins per mutant, with that file's cases
// asserting strictly more (option counts, selected counts, both localizers).
// Two suites over one helper had to be reconciled on every view-model change,
// so this file keeps what it is named for: the rendered settings section.
