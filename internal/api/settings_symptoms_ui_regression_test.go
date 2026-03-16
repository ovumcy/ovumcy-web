package api

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/terraincognita07/ovumcy/internal/models"
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
