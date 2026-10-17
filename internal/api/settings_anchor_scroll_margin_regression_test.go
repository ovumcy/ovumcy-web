package api

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/templates"
	staticassets "github.com/ovumcy/ovumcy-web/web"
	"golang.org/x/net/html"
)

// A settings deep link — the section index, the dashboard's "update cycle
// data" button, the privacy page's ledger link, the account chip — scrolls its
// target card to the top of the viewport. The sticky paper-header sits there,
// and from 640px up so does the sticky section index, so a card with no
// scroll-margin-top lands with its own title underneath ~220px of chrome
// (WEB-222). The margin lives on the settings-section utility, so every anchor
// target has to carry that class, and the served stylesheet has to carry the
// rule that reads the chrome's height.
var settingsDeepLinkPattern = regexp.MustCompile(`href="/settings#([a-z0-9-]+)"`)

func TestSettingsAnchorTargetsClearTheStickyChrome(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-anchor-margin@example.com")
	document := mustParseHTMLDocument(t, renderSettingsPageForTest(t, ctx.app, ctx.authCookie))

	nav := htmlElementByTagAndClass(document, "nav", "settings-section-nav")
	if nav == nil {
		t.Fatal("expected the settings section index")
	}
	// The script that measures the index's wrapped height finds it by this hook.
	if !htmlHasAttr(nav, "data-settings-section-nav") {
		t.Fatal("the section index lost data-settings-section-nav, so its measured height is never published and a wrapped index covers the card titles again")
	}

	targets := make([]string, 0, 16)
	for _, link := range htmlFindElements(nav, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a"
	}) {
		targets = append(targets, strings.TrimPrefix(htmlAttr(link, "href"), "#"))
	}

	// Links from other pages land on the same cards and are held to the same
	// margin; they are collected from the template sources so a link added
	// tomorrow is covered without extending this list.
	deepLinks := make([]string, 0, 4)
	err := fs.WalkDir(templates.Files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		source, readErr := templates.Files.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range settingsDeepLinkPattern.FindAllStringSubmatch(string(source), -1) {
			deepLinks = append(deepLinks, match[1])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the template sources: %v", err)
	}
	// Anti-vacuity, by name: the dashboard's cycle link and the privacy page's
	// ledger link are the cross-page deep links the defect was reported on.
	for _, required := range []string{"settings-cycle", "settings-egress"} {
		if !slices.Contains(deepLinks, required) {
			t.Fatalf("the template scan found no /settings#%s link (found %v); the scan itself is broken", required, deepLinks)
		}
	}
	targets = append(targets, deepLinks...)

	for _, id := range targets {
		target := htmlElementByID(document, id)
		if target == nil {
			t.Errorf("a settings link points at #%s, which the settings page does not render", id)
			continue
		}
		if !htmlHasClass(target, "settings-section") {
			t.Errorf("#%s is an anchor target without the settings-section class, so a jump to it lands its title under the sticky header and section index", id)
		}
	}

	stylesheet, err := staticassets.Files.ReadFile("static/css/tailwind.css")
	if err != nil {
		t.Fatalf("read the embedded stylesheet: %v", err)
	}
	rules := string(stylesheet)
	for _, want := range []struct{ fragment, why string }{
		{
			".settings-section{scroll-margin-top:calc(var(--sticky-header-clearance) + .75rem)}",
			"a phone-width jump must clear the sticky header",
		},
		{
			".settings-section{scroll-margin-top:calc(var(--sticky-header-clearance) + var(--settings-section-nav-height) + .75rem)}",
			"a wide jump must clear the header and the sticky section index",
		},
		{
			"top:var(--sticky-header-clearance)",
			"the section index must stick at the same offset the cards clear",
		},
	} {
		if !strings.Contains(rules, want.fragment) {
			t.Errorf("the served stylesheet lacks %q: %s", want.fragment, want.why)
		}
	}
	stackRule, found := cutCSSRule(rules, "[data-settings-sections]{")
	if !found {
		t.Fatal("the served stylesheet declares no [data-settings-sections] rule, so the chrome-height variables resolve to nothing and the margin collapses to zero")
	}
	for _, variable := range []string{"--sticky-header-clearance:", "--settings-section-nav-height:"} {
		if !strings.Contains(stackRule, variable) {
			t.Errorf("the settings stack rule declares no %s default: %s", strings.TrimSuffix(variable, ":"), stackRule)
		}
	}
}
