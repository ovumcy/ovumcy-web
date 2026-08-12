package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

func TestDashboardLogoutFormsRequireConfirmation(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "logout-confirm@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	rendered := string(body)

	logoutForms := regexp.MustCompile(`(?is)<form\b[^>]*action="/logout"[^>]*>`).FindAllString(rendered, -1)
	if len(logoutForms) < 2 {
		t.Fatalf("expected dashboard navigation to render both desktop and mobile logout forms")
	}
	for _, form := range logoutForms {
		if !strings.Contains(form, `method="post"`) {
			t.Fatalf("expected logout form to use POST, got %q", form)
		}
		if !strings.Contains(form, `data-confirm="`) {
			t.Fatalf("expected logout form to carry confirmation wiring, got %q", form)
		}
	}
	if strings.Contains(rendered, `action="/api/v1/sessions/current"`) {
		t.Fatalf("did not expect dashboard navigation to post to the raw API logout route")
	}
	if got := len(regexp.MustCompile(`(?is)<input\b[^>]*name="csrf_token"[^>]*>`).FindAllString(rendered, -1)); got < len(logoutForms) {
		t.Fatalf("expected csrf token hidden fields for each logout form, got %d for %d forms", got, len(logoutForms))
	}
}

func TestDashboardNavigationShowsDisplayNameWithoutEmailFallback(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "identity-owner@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("display_name", "Maya").Error; err != nil {
		t.Fatalf("seed display name: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	rendered := string(body)
	if strings.Contains(rendered, "identity-owner") {
		t.Fatalf("did not expect local-part identity in navigation")
	}
	if strings.Contains(rendered, "identity-owner@example.com") {
		t.Fatalf("did not expect email identity in navigation")
	}
	if strings.Count(rendered, `data-current-user-identity`) != 2 {
		t.Fatalf("expected both dashboard nav identity chips to render the saved display name, got %q", rendered)
	}
	if strings.Contains(rendered, `nav-user-chip-empty`) {
		t.Fatalf("did not expect empty identity chip styling when display name exists, got %q", rendered)
	}
	for _, id := range []string{`id="nav-user-chip-desktop"`, `id="nav-user-chip-mobile"`} {
		if !strings.Contains(rendered, id) {
			t.Fatalf("expected dashboard navigation chip %s in response", id)
		}
	}
}

func TestDashboardNavigationShowsProfileHintWhenDisplayNameEmpty(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "identity-empty@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	rendered := mustRenderDashboard(t, app, authCookie, "en")
	if strings.Contains(rendered, "identity-empty@example.com") || strings.Contains(rendered, "identity-empty") {
		t.Fatalf("did not expect email fallback in navigation when display name is empty")
	}
	if strings.Count(rendered, `nav-user-chip-empty`) < 2 {
		t.Fatalf("expected both dashboard nav identity chips to use the empty-state styling, got %q", rendered)
	}
	if strings.Contains(rendered, `data-current-user-identity`) {
		t.Fatalf("did not expect display-name identity spans when display name is empty, got %q", rendered)
	}
	for _, id := range []string{`id="nav-user-chip-desktop"`, `id="nav-user-chip-mobile"`} {
		if !strings.Contains(rendered, id) {
			t.Fatalf("expected dashboard navigation chip %s in response", id)
		}
	}
}

func TestDashboardHeaderOmitsLanguageSwitch(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "lang-switch-labels@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	rendered := mustRenderDashboard(t, app, authCookie, "ru")
	for _, label := range []string{"RU", "EN", "ES"} {
		if strings.Contains(rendered, ">"+label+"</a>") {
			t.Fatalf("did not expect %s language shortcut in dashboard header", label)
		}
	}
}

func TestDashboardYesterdayLinkTargetsCalendarEditModeForSelectedDay(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "yesterday-link@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	rendered := mustRenderDashboard(t, app, authCookie, "en")
	yesterdayLinkPattern := regexp.MustCompile(`href="/calendar\?month=\d{4}-\d{2}&day=\d{4}-\d{2}-\d{2}&edit=1"`)
	if !yesterdayLinkPattern.MatchString(rendered) {
		t.Fatalf("expected yesterday link to target calendar selected day edit mode, got %q", rendered)
	}
	if strings.Contains(rendered, `selected=`) {
		t.Fatalf("did not expect legacy selected query parameter in dashboard links")
	}
}

func TestCalendarSelectedDayLoadsEditModeWhenRequested(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-edit-selected@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/calendar?month=2026-03&day=2026-03-12&edit=1", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	if !strings.Contains(rendered, `data-selected-date="2026-03-12"`) {
		t.Fatalf("expected calendar page to keep the selected day in the view state, got %q", rendered)
	}
	dayEditor := htmlElementByID(mustParseHTMLDocument(t, rendered), "day-editor")
	if dayEditor == nil {
		t.Fatalf("expected a #day-editor container on the calendar page, got %q", rendered)
	}
	loader := htmlElementByAttr(dayEditor, "hx-trigger", "load")
	if loader == nil {
		t.Fatalf("expected #day-editor to hold an hx-trigger=load lazy-loader, got %q", rendered)
	}
	if got := htmlAttr(loader, "hx-get"); got != "/calendar/day/2026-03-12?mode=edit" {
		t.Fatalf("expected the #day-editor lazy-loader to fetch edit mode from /calendar/day/2026-03-12?mode=edit, got hx-get=%q", got)
	}
}

// navSectionHooks lists the section destinations a navigation landmark offers,
// in document order, by their stable data-nav-link hooks.
func navSectionHooks(root *html.Node) []string {
	links := htmlFindElements(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-nav-link")
	})
	hooks := make([]string, 0, len(links))
	for _, link := range links {
		hooks = append(hooks, htmlAttr(link, "data-nav-link"))
	}
	return hooks
}

// desktopPrimaryNavigation returns the header's desktop navigation landmark —
// the only <nav> that holds the account actions block.
func desktopPrimaryNavigation(root *html.Node) *html.Node {
	return htmlFindElement(root, func(node *html.Node) bool {
		if node.Type != html.ElementNode || node.Data != "nav" {
			return false
		}
		return htmlFindElement(node, func(candidate *html.Node) bool {
			return candidate.Type == html.ElementNode && htmlHasAttr(candidate, "data-nav-account-actions")
		}) != nil
	})
}

// settingsHrefTargets lists, in document order, the href values under root that
// lead to the settings page. A bare path, a query and a section fragment are
// the same destination as far as a reader of the navigation is concerned.
func settingsHrefTargets(root *html.Node) []string {
	links := htmlFindElements(root, func(node *html.Node) bool {
		if node.Type != html.ElementNode || !htmlHasAttr(node, "href") {
			return false
		}
		path, _, _ := strings.Cut(htmlAttr(node, "href"), "#")
		path, _, _ = strings.Cut(path, "?")
		return path == "/settings" || strings.HasPrefix(path, "/settings/")
	})
	targets := make([]string, 0, len(links))
	for _, link := range links {
		targets = append(targets, htmlAttr(link, "href"))
	}
	return targets
}

// TestDesktopHeaderOffersASingleSettingsEntry pins the one-entry contract for
// the desktop header. It used to carry a gear icon link and the account
// identity chip side by side — two controls in the same navigation landmark
// leading to the same page, with the gear adding nothing the chip does not
// already offer. The chip is the survivor: it reaches settings and names the
// account, so it must keep an accessible name of its own (its avatar glyph is
// decorative and stays out of the accessibility tree).
func TestDesktopHeaderOffersASingleSettingsEntry(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "single-settings-entry@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for _, path := range []string{"/dashboard", "/calendar", "/stats", "/settings"} {
		document := mustParseHTMLDocument(t, fetchPageBody(t, app, path, authCookie))

		desktop := desktopPrimaryNavigation(document)
		if desktop == nil {
			t.Fatalf("%s: expected the desktop primary navigation to render", path)
		}

		targets := settingsHrefTargets(desktop)
		if len(targets) != 1 {
			t.Errorf(
				"%s: the desktop header offers %d controls leading to settings (%v); exactly one must",
				path, len(targets), targets,
			)
		}

		entry := htmlElementByID(desktop, "nav-user-chip-desktop")
		if entry == nil {
			t.Fatalf("%s: the surviving desktop settings entry must be the account identity chip", path)
		}
		if got := strings.TrimSpace(htmlAttr(entry, "aria-label")); got == "" {
			t.Errorf("%s: the account identity chip must carry an accessible name", path)
		}
	}
}

// TestMobileHeaderMenuCarriesOnlyAccountEntries pins the single-navigation
// contract for mobile: the header menu used to repeat Today / Calendar /
// Insights / Settings, so a phone carried two navigations to the same four
// destinations — the menu pushed the page down when opened and left the bottom
// tab bar visible underneath it. The four section destinations now live in the
// tab bar alone; the header menu is the account menu (profile chip + logout)
// and declares that reduced scope through data-mobile-menu="account". Desktop
// keeps its own primary navigation, unaffected by that reduction.
func TestMobileHeaderMenuCarriesOnlyAccountEntries(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "mobile-single-nav@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	sections := []string{"today", "calendar", "insights", "settings"}
	// Desktop reaches settings through the account chip (an account control, not
	// a section link), so its section hooks stop at the three page destinations —
	// TestDesktopHeaderOffersASingleSettingsEntry pins the settings route itself.
	desktopSections := []string{"today", "calendar", "insights"}

	for _, path := range []string{"/dashboard", "/calendar", "/stats", "/settings"} {
		document := mustParseHTMLDocument(t, fetchPageBody(t, app, path, authCookie))

		menu := htmlFindElement(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "nav" && htmlHasAttr(node, "data-mobile-menu")
		})
		if menu == nil {
			t.Fatalf("%s: expected the header to render the mobile menu landmark", path)
		}
		if got := htmlAttr(menu, "data-mobile-menu"); got != "account" {
			t.Errorf("%s: mobile menu declares data-mobile-menu=%q, expected \"account\"", path, got)
		}
		if hooks := navSectionHooks(menu); len(hooks) != 0 {
			t.Errorf(
				"%s: the mobile header menu still lists section destinations %v; the bottom tab bar already covers them, so a phone gets two navigations to the same places",
				path, hooks,
			)
		}
		if htmlElementByID(menu, "nav-user-chip-mobile") == nil {
			t.Errorf("%s: the mobile menu must keep the profile entry", path)
		}
		if htmlElementByAttr(menu, "action", "/logout") == nil {
			t.Errorf("%s: the mobile menu must keep the logout form", path)
		}

		tabbar := htmlElementByTagAndClass(document, "nav", "mobile-tabbar")
		if tabbar == nil {
			t.Fatalf("%s: expected the bottom tab bar to stay in place", path)
		}
		if got := navSectionHooks(tabbar); !slices.Equal(got, sections) {
			t.Errorf("%s: the bottom tab bar must remain the mobile route to every section, got %v", path, got)
		}

		desktop := desktopPrimaryNavigation(document)
		if desktop == nil {
			t.Fatalf("%s: expected the desktop primary navigation to render", path)
		}
		if got := navSectionHooks(desktop); !slices.Equal(got, desktopSections) {
			t.Errorf("%s: desktop navigation must keep every section link, got %v", path, got)
		}
		if htmlElementByID(desktop, "nav-user-chip-desktop") == nil {
			t.Errorf("%s: the desktop navigation must keep the account chip, its only route to settings", path)
		}
	}
}

func mustRenderDashboard(t *testing.T, app *fiber.App, authCookie string, languageCookie string) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	if strings.TrimSpace(languageCookie) == "" {
		request.Header.Set("Cookie", authCookie)
	} else {
		request.Header.Set("Cookie", authCookie+"; ovumcy_lang="+languageCookie)
	}

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	return string(body)
}
