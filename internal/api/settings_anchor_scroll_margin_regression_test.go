package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strconv"
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

	// A no-script redirect back to /settings lands on its fragment the same way.
	redirects := settingsRedirectFragments(t)
	// Anti-vacuity, by declaration: the Interface card's redirect is the site
	// the status-anchor targets were added for.
	if !slices.Contains(redirects, strings.TrimPrefix(interfaceCardFragment, "#")) {
		t.Fatalf("the handler scan did not find interfaceCardFragment (found %v); the scan itself is broken", redirects)
	}
	targets = append(targets, redirects...)

	for _, id := range targets {
		target := htmlElementByID(document, id)
		if target == nil {
			t.Errorf("a settings link points at #%s, which the settings page does not render", id)
			continue
		}
		// A status island is a valid target too: settings-status-anchor gives it
		// the same chrome clearance as a whole card.
		if !htmlHasClass(target, "settings-section") && !htmlHasClass(target, "settings-status-anchor") {
			t.Errorf("#%s is an anchor target with neither settings-section nor settings-status-anchor, so a jump to it lands under the sticky header and section index", id)
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
			".settings-status-anchor{scroll-margin-top:calc(var(--sticky-header-clearance) + .75rem)}",
			"a phone-width jump to a status island must clear the sticky header",
		},
		{
			".settings-status-anchor{scroll-margin-top:calc(var(--sticky-header-clearance) + var(--settings-section-nav-height) + .75rem)}",
			"a wide jump to a status island must clear the header and the sticky section index",
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

// settingsRedirectFragments collects the #settings-* fragments the transport's
// non-test sources append to a /settings destination. A literal passed as an
// HX-Retarget or HX-Reselect value is a CSS selector for a swap, not a scroll
// target, and is skipped.
func settingsRedirectFragments(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("list the api sources: %v", err)
	}
	fset := token.NewFileSet()
	var found []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		headerValues := map[*ast.BasicLit]bool{}
		ast.Inspect(file, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && len(call.Args) == 2 {
				// HX-Redirect and HX-Location navigate and scroll, so they stay in.
				if header, ok := call.Args[0].(*ast.BasicLit); ok && (header.Value == `"HX-Retarget"` || header.Value == `"HX-Reselect"`) {
					if value, ok := call.Args[1].(*ast.BasicLit); ok {
						headerValues[value] = true
					}
				}
			}
			return true
		})
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || headerValues[lit] {
				return true
			}
			value, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				return true
			}
			prefix, fragment, hasFragment := strings.Cut(value, "#")
			if hasFragment && (prefix == "" || prefix == "/settings") && strings.HasPrefix(fragment, "settings-") {
				found = append(found, fragment)
			}
			return true
		})
	}
	return found
}
