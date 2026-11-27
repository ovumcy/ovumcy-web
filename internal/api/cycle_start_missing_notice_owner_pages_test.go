package api

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// cycleStartMissingOwnerPages are the three owner pages that carry the shared
// no-cycle-start notice.
var cycleStartMissingOwnerPages = []string{"/dashboard", "/calendar", "/stats"}

func cycleStartMissingNoticeLink(notice *html.Node, attr string) *html.Node {
	return htmlFindElement(notice, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && htmlHasAttr(node, attr)
	})
}

// TestCycleStartMissingNoticeOnEveryOwnerPage pins the way back in for an
// account with no cycle start — the state a skipped onboarding date and a
// clear-data wipe both leave, where /onboarding is no longer reachable: the
// dashboard, calendar and stats pages show one notice whose secondary action is
// the settings card holding the start-date field, and whose primary action is
// the calendar everywhere except on the calendar itself.
func TestCycleStartMissingNoticeOnEveryOwnerPage(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "start-date-reentry@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for _, path := range cycleStartMissingOwnerPages {
		document := fetchLateCycleNoticePage(t, app, authCookie, path)
		notices := collectHTMLNodesWithAttr(document, "data-cycle-start-missing-notice")
		if len(notices) != 1 {
			t.Fatalf("%s rendered %d no-cycle-start notices, want exactly 1", path, len(notices))
		}
		notice := notices[0]
		if !strings.Contains(htmlNodeText(notice), "Add the first day of your last period") {
			t.Fatalf("%s notice does not carry the cycle_start_missing.body copy: %q", path, htmlNodeText(notice))
		}
		settings := cycleStartMissingNoticeLink(notice, "data-cycle-start-missing-settings")
		if settings == nil || htmlAttr(settings, "href") != "/settings#settings-cycle" {
			t.Fatalf("%s notice has no settings action to /settings#settings-cycle", path)
		}
		calendar := cycleStartMissingNoticeLink(notice, "data-cycle-start-missing-calendar")
		if path == "/calendar" {
			if calendar != nil {
				t.Fatal("the calendar's notice links to the calendar itself; the grid is the action there")
			}
		} else if calendar == nil || htmlAttr(calendar, "href") != "/calendar" {
			t.Fatalf("%s notice has no primary action to /calendar", path)
		}
		// The notice is the page's one explanation: the first-cycle explainer
		// would claim a setup cycle length stands behind estimates that do not
		// exist yet.
		if explainer := findHTMLNodeWithAttr(document, "data-explainer-key"); explainer != nil {
			t.Fatalf("%s still renders the prediction explainer %q beside the notice", path, htmlAttr(explainer, "data-explainer-key"))
		}
	}

	stats := fetchLateCycleNoticePage(t, app, authCookie, "/stats")
	if findHTMLNodeWithAttr(stats, "data-stats-empty-state") != nil {
		t.Fatal("stats rendered the completed-cycle empty state beside the notice; the notice takes its place")
	}

	settings := fetchLateCycleNoticePage(t, app, authCookie, "/settings")
	card := htmlFindElement(settings, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "id") == "settings-cycle"
	})
	if card == nil {
		t.Fatal("settings page no longer renders the settings-cycle card the notice links to")
	}
	field := htmlFindElement(card, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "id") == "settings-last-period-start"
	})
	if field == nil {
		t.Fatal("expected the settings-cycle card to hold the start-date field")
	}
}

// TestCycleStartMissingNoticeLeavesOnceAStartExists: the notice is not
// dismissible, so the only way it goes is a recorded start, and a recorded
// start must take it off every page.
func TestCycleStartMissingNoticeLeavesOnceAStartExists(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	_, authCookie, _ := newStatsOverviewOwner(t, app, database, "start-date-present@example.com")

	for _, path := range cycleStartMissingOwnerPages {
		document := fetchLateCycleNoticePage(t, app, authCookie, path)
		if findHTMLNodeWithAttr(document, "data-cycle-start-missing-notice") != nil {
			t.Fatalf("%s rendered the no-cycle-start notice for an account with a recorded start", path)
		}
	}
}

// TestCycleStartMissingNoticeStaysOffWherePredictionIsOff: the notice promises
// predictions, so an account that turned predictions off keeps its own
// explainer instead.
func TestCycleStartMissingNoticeStaysOffWherePredictionIsOff(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "start-date-unpredictable@example.com", "StrongPass1", true)
	updateStatsOverviewUser(t, database, user, map[string]any{"unpredictable_cycle": true})
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for _, path := range cycleStartMissingOwnerPages {
		document := fetchLateCycleNoticePage(t, app, authCookie, path)
		if findHTMLNodeWithAttr(document, "data-cycle-start-missing-notice") != nil {
			t.Fatalf("%s promised predictions to an account that turned them off", path)
		}
	}
}
