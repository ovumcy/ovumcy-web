package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/ovumcy/ovumcy-web/internal/templates"
	"golang.org/x/net/html"
)

// These tests pin the a11y contract from audit task #26-A2: every HTMX
// save-status container that receives swapped success/error feedback must be
// an aria-live="polite" region, so screen-reader users hear the outcome of
// day saves, onboarding steps, and settings submissions instead of the page
// staying silent. The auth pages already carried aria-live; this locks the
// same treatment onto the dashboard, calendar editor, onboarding, and
// settings surfaces.

func assertLiveStatusContainers(t *testing.T, body string, ids ...string) {
	t.Helper()
	document := mustParseHTMLDocument(t, body)
	for _, id := range ids {
		targetID := id
		node := htmlFindElement(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "id") == targetID
		})
		if node == nil {
			t.Fatalf("expected an element with id %q", targetID)
		}
		// The a11y contract is only id + aria-live=polite; the element's class
		// list and attribute ordering are incidental styling, not the contract.
		if got := htmlAttr(node, "aria-live"); got != "polite" {
			t.Fatalf("expected %q to be an aria-live=polite region, got aria-live=%q", targetID, got)
		}
	}
}

func fetchPageBody(t *testing.T, app *fiber.App, path string, authCookie string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}

func TestDashboardSaveStatusIsLiveRegion(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-dashboard-live@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body := fetchPageBody(t, app, "/dashboard", authCookie)
	assertLiveStatusContainers(t, body, "save-status")
}

func TestCalendarDayEditorSaveStatusIsLiveRegion(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-calendar-live@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body := fetchPageBody(t, app, "/calendar/day/2026-02-17?mode=edit", authCookie)
	assertLiveStatusContainers(t, body, "calendar-save-status")
}

func TestOnboardingStepStatusesAreLiveRegions(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-onboarding-live@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body := fetchPageBody(t, app, "/onboarding", authCookie)
	assertLiveStatusContainers(t, body, "onboarding-step1-status", "onboarding-step2-status")
}

// TestBaseLayoutRendersSkipLink pins the skip-to-content link from audit
// task #26: every full page rendered through the base layout must offer
// keyboard users a way past the always-visible header and navigation
// (Tailwind's content scan reads these comments too, so utility-named
// bare words are avoided here on purpose), and the link's
// target must be focusable (tabindex=-1 on <main>) so the jump actually
// moves focus.
func TestBaseLayoutRendersSkipLink(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-skip-link@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for _, path := range []string{"/dashboard", "/settings"} {
		document := mustParseHTMLDocument(t, fetchPageBody(t, app, path, authCookie))
		// Structural hooks via semantic DOM — the visible copy is i18n
		// (a11y.skip_to_content) and the actual skip behavior is owned by
		// visual-a11y.spec.ts; do not pin the English phrase here.
		skipLink := htmlFindElement(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "a" &&
				htmlAttr(node, "href") == "#main-content" && htmlHasClass(node, "skip-link")
		})
		if skipLink == nil {
			t.Fatalf("expected %s to render the skip-to-content link", path)
		}
		mainTarget := htmlFindElement(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "main" &&
				htmlAttr(node, "id") == "main-content" && htmlAttr(node, "tabindex") == "-1"
		})
		if mainTarget == nil {
			t.Fatalf("expected %s to render a focusable #main-content target", path)
		}
	}
}

// isDecorativeGlyph reports whether a rune is one of the pictographs, arrows,
// geometric icons and emoji this UI uses as decoration. Ordinary typography a
// sentence may legitimately contain — the bullet, the middle dot, the degree
// sign — is deliberately outside every range below.
func isDecorativeGlyph(candidate rune) bool {
	switch {
	case candidate >= 0x2190 && candidate <= 0x2BFF:
		return true
	case candidate == 0x3030:
		return true
	case candidate >= 0x1F000 && candidate <= 0x1FAFF:
		return true
	default:
		return false
	}
}

// TestTemplatesKeepDecorativeGlyphsOutOfTheAccessibilityTree kills the class
// behind an audit finding: 48 of the 58 distinct emoji this UI renders reached
// the accessibility tree as content, so a screen reader read out "shield",
// "file cabinet", "hourglass", "see-no-evil monkey", "crystal ball" and
// "electric plug" ahead of the headings they decorate.
//
// A decorative glyph therefore lives in its own element carrying
// aria-hidden="true"; where the glyph is the whole content of a control, the
// control names itself (aria-label) instead. Scanning the embedded template
// sources rather than a rendered page covers a surface added tomorrow without
// anyone remembering to extend this test.
func TestTemplatesKeepDecorativeGlyphsOutOfTheAccessibilityTree(t *testing.T) {
	var scanned int
	err := fs.WalkDir(templates.Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		source, readErr := templates.Files.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++

		// Splitting on '<' yields chunks of "tag attributes>text": the text of a
		// chunk is painted by the element that chunk opens, so a glyph in that
		// text is exposed unless the element it sits in hides itself. A glyph
		// following a closing tag has no such element and fails here too, which
		// is the point — it is exposed exactly the same way.
		for _, chunk := range strings.Split(string(source), "<") {
			tag, text, found := strings.Cut(chunk, ">")
			if !found {
				tag, text = "", chunk
			}
			if !strings.ContainsFunc(text, isDecorativeGlyph) {
				continue
			}
			if strings.Contains(tag, `aria-hidden="true"`) {
				continue
			}
			t.Errorf(
				"%s: a decorative glyph is exposed to assistive technology.\n"+
					"Wrap it in an element carrying aria-hidden=\"true\" (and give the control its own aria-label when the glyph is all it contains).\n"+
					"element: <%s>, text: %s",
				path, strings.TrimSpace(tag), strings.TrimSpace(text),
			)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded templates: %v", err)
	}
	if scanned == 0 {
		t.Fatal("no templates were scanned: the embedded template FS layout changed and this guard silently stopped checking anything")
	}
}

// TestAuthenticatedPagesNameEveryNavigationLandmark pins the fix for the axe
// landmark-unique finding: an authenticated page renders four to five <nav>
// elements (desktop header, mobile menu, settings section list, bottom tab bar,
// footer) and unnamed ones are indistinguishable in a landmark list. Every one
// carries a localized aria-label, and no two share it.
func TestAuthenticatedPagesNameEveryNavigationLandmark(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-landmarks@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for _, path := range []string{"/dashboard", "/settings"} {
		document := mustParseHTMLDocument(t, fetchPageBody(t, app, path, authCookie))
		landmarks := htmlFindElements(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "nav"
		})
		if len(landmarks) < 4 {
			t.Fatalf("%s: expected the layout to render at least four nav landmarks, got %d", path, len(landmarks))
		}
		seen := make(map[string]bool, len(landmarks))
		for index, landmark := range landmarks {
			label := htmlAttr(landmark, "aria-label")
			if strings.TrimSpace(label) == "" {
				t.Errorf("%s: nav landmark #%d carries no aria-label, so a landmark list cannot tell it apart", path, index)
				continue
			}
			if seen[label] {
				t.Errorf("%s: nav landmark #%d repeats the accessible name %q", path, index, label)
			}
			seen[label] = true
		}
	}
}

// TestPasswordManagerUsernameFieldsStayOutOfTheAccessibilityTree records the
// answer to an audit finding rather than a change: the visually hidden email
// input each password form carries for password-manager association is a field
// no one is meant to fill, so it is hidden from assistive technology
// (aria-hidden) and from the tab order (tabindex=-1, readonly) instead of being
// given a label. Password managers read autocomplete="username" off the DOM, so
// the association survives. Dropping aria-hidden would reintroduce an
// unlabelled edit field (WCAG 3.3.2); dropping autocomplete would break the
// association it exists for.
func TestPasswordManagerUsernameFieldsStayOutOfTheAccessibilityTree(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-username-field@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	document := mustParseHTMLDocument(t, fetchPageBody(t, app, "/settings", authCookie))
	fields := htmlFindElements(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "input" &&
			htmlAttr(node, "autocomplete") == "username"
	})
	if len(fields) == 0 {
		t.Fatal("expected the settings password forms to carry password-manager username fields")
	}
	for index, field := range fields {
		if got := htmlAttr(field, "aria-hidden"); got != "true" {
			t.Errorf("username field #%d reaches the accessibility tree as an unlabelled edit field (aria-hidden=%q)", index, got)
		}
		if got := htmlAttr(field, "tabindex"); got != "-1" {
			t.Errorf("username field #%d is reachable by Tab (tabindex=%q), which aria-hidden must never hide", index, got)
		}
		if !htmlHasAttr(field, "readonly") {
			t.Errorf("username field #%d is not readonly, so a hidden field could still take input", index)
		}
	}
}

// TestConfirmDialogIsRemovedFromTheAccessibilityTreeWhileClosed records the
// second audit answer: #confirm-modal-cancel and #confirm-modal-accept are
// empty until the dialog opens and reads its captions off the invoking
// element's data attributes, which looks like two unnamed buttons. They are
// not: the closed dialog is display:none (the `hidden` class) *and*
// aria-hidden="true", so neither button is in the accessibility tree at all.
// Both halves are pinned here — removing either would expose them.
func TestConfirmDialogIsRemovedFromTheAccessibilityTreeWhileClosed(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-confirm-modal@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	document := mustParseHTMLDocument(t, fetchPageBody(t, app, "/dashboard", authCookie))
	modal := htmlElementByID(document, "confirm-modal")
	if modal == nil {
		t.Fatal("expected the base layout to render #confirm-modal")
	}
	if !htmlHasClass(modal, "hidden") {
		t.Error("the closed confirm dialog is not display:none, so its empty buttons reach the accessibility tree")
	}
	if got := htmlAttr(modal, "aria-hidden"); got != "true" {
		t.Errorf("the closed confirm dialog declares aria-hidden=%q, expected \"true\"", got)
	}
}

// isInterfaceEmoji reports whether a rune is an emoji pictograph — the class
// the first-party icon set replaced. Three tells, none of them a list of the
// emoji that happened to be here: a codepoint in the pictograph planes, the
// emoji presentation selector that turns a symbol into a colour glyph, and the
// symbol/dingbat blocks. The two exceptions are typography rather than
// pictographs and are drawn by the font at every size: the heart the calendar
// marks a logged intimacy with, and the check mark a save confirms with.
func isInterfaceEmoji(candidate rune) bool {
	switch candidate {
	case 0x2665, 0x2713:
		return false
	case 0xFE0F, 0x3030:
		return true
	}
	switch {
	case candidate >= 0x1F000 && candidate <= 0x1FAFF:
		return true
	case candidate >= 0x2600 && candidate <= 0x27BF:
		return true
	default:
		return false
	}
}

var iconReferencePattern = regexp.MustCompile(`{{template "icon" "([a-z-]+)"}}`)

// spriteSymbolNames reads the icon names the sprite defines.
func spriteSymbolNames(t *testing.T) map[string]bool {
	t.Helper()
	source, err := templates.Files.ReadFile("components/icons.html")
	if err != nil {
		t.Fatalf("read the icon sprite: %v", err)
	}
	names := make(map[string]bool)
	for _, match := range regexp.MustCompile(`<symbol id="icon-([a-z-]+)"`).FindAllStringSubmatch(string(source), -1) {
		names[match[1]] = true
	}
	if len(names) == 0 {
		t.Fatal("the icon sprite defines no symbols: its markup changed and this guard stopped checking anything")
	}
	return names
}

// TestInterfaceGlyphsComeFromTheFirstPartyIconSet pins wave 3 item 12. Interface
// chrome — navigation, buttons, section markers, toggles, banners — is drawn by
// the inline SVG sprite rather than by emoji, which rendered as a different
// picture on every platform and reached assistive technology as words. The
// guard has three halves: no template carries an emoji pictograph any more,
// every icon a template asks for exists in the sprite, and so does every icon
// name the phase policy hands the templates.
func TestInterfaceGlyphsComeFromTheFirstPartyIconSet(t *testing.T) {
	symbols := spriteSymbolNames(t)

	var scanned int
	err := fs.WalkDir(templates.Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		source, readErr := templates.Files.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++

		for _, line := range strings.Split(string(source), "\n") {
			if !strings.ContainsFunc(line, isInterfaceEmoji) {
				continue
			}
			t.Errorf(
				"%s: an interface glyph is an emoji.\n"+
					"Draw it in the icon set (components/icons.html) and reference it with {{template \"icon\" \"<name>\"}}.\n"+
					"line: %s",
				path, strings.TrimSpace(line),
			)
		}
		for _, match := range iconReferencePattern.FindAllStringSubmatch(string(source), -1) {
			if !symbols[match[1]] {
				t.Errorf("%s: references the icon %q, which the sprite does not define", path, match[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded templates: %v", err)
	}
	if scanned == 0 {
		t.Fatal("no templates were scanned: the embedded template FS layout changed and this guard silently stopped checking anything")
	}

	// The phase chip picks its icon in the services layer, so a rename there
	// would ship a blank chip that no template-only scan can see.
	for _, phase := range []string{"menstrual", "follicular", "ovulation", "luteal", "unknown"} {
		if name := services.PhaseIcon(phase); !symbols[name] {
			t.Errorf("PhaseIcon(%q) names the icon %q, which the sprite does not define", phase, name)
		}
	}
}

// TestBaseLayoutInlinesTheIconSprite keeps the sprite in the document every
// icon references: <use> resolves same-document only here, by design — an
// external sprite file would need a request the strict CSP is meant to make
// unnecessary — so a page without it renders every icon blank.
func TestBaseLayoutInlinesTheIconSprite(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-icon-sprite@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body := fetchPageBody(t, app, "/dashboard", authCookie)
	if !strings.Contains(body, `id="icon-drop"`) {
		t.Error("the dashboard renders no icon sprite, so every icon on the page resolves to nothing")
	}
	if !strings.Contains(body, `href="#icon-drop"`) {
		t.Error("the dashboard references no sprite icon, so the period quick action lost its glyph")
	}
}

func TestSettingsStatusContainersAreLiveRegions(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-settings-live@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body := fetchPageBody(t, app, "/settings", authCookie)
	assertLiveStatusContainers(t, body,
		"settings-cycle-status",
		"settings-tracking-status",
		"settings-clear-data-status",
		"delete-account-feedback",
	)
}
