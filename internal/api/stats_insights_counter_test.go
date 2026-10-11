package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const awaitingMoreCyclesExplainerKey = "prediction.explainer.awaiting_more_cycles"

// TestStatsInsightsGateCountsCompletedCyclesLikeTheDashboard seeds a history
// whose newest cycle starts today. The dashboard's fertility floor counts the
// cycle that start closes (CycleStats.CompletedCycleCount); the trend chart's
// own series stops at starts strictly before today and so counts one fewer. The
// stats page's gate and its "n / N" progress line read the dashboard's counter,
// so the two pages tell the owner one count and one tier.
func TestStatsInsightsGateCountsCompletedCyclesLikeTheDashboard(t *testing.T) {
	for index, testCase := range []struct {
		name       string
		startsBack []int
		wantCount  string
		wantLocked bool
	}{
		{name: "two completed cycles stay locked and say two", startsBack: []int{56, 28, 0}, wantCount: "2", wantLocked: true},
		{name: "three completed cycles unlock", startsBack: []int{84, 56, 28, 0}, wantCount: "3", wantLocked: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user, authCookie, today := newStatsOverviewOwner(t, app, database, fmt.Sprintf("stats-gate-counter-%d@example.com", index))
			seedStatsOverviewCycleHistory(t, database, user, today, testCase.startsBack...)
			updateStatsOverviewUser(t, database, user, map[string]any{"last_period_start": today})

			statsDocument := fetchLateCycleNoticePage(t, app, authCookie, "/stats")
			emptyState := findHTMLNodeWithAttr(statsDocument, "data-stats-empty-state")
			if locked := emptyState != nil; locked != testCase.wantLocked {
				t.Fatalf("stats page locked = %v, want %v with %s completed cycles", locked, testCase.wantLocked, testCase.wantCount)
			}
			if testCase.wantLocked {
				if got := htmlAttr(emptyState, "data-stats-completed-cycles"); got != testCase.wantCount {
					t.Fatalf("stats progress counts %q completed cycles, want %q: the dashboard counts one more than the trend series does", got, testCase.wantCount)
				}
			}

			dashboardDocument := fetchLateCycleNoticePage(t, app, authCookie, "/dashboard")
			explainer := findHTMLNodeWithAttr(dashboardDocument, "data-dashboard-prediction-explainer")
			dashboardAwaiting := explainer != nil && htmlAttr(explainer, "data-explainer-key") == awaitingMoreCyclesExplainerKey
			if dashboardAwaiting != testCase.wantLocked {
				t.Fatalf("dashboard awaiting-more-cycles tier = %v while the stats page locked = %v: the two pages count differently", dashboardAwaiting, testCase.wantLocked)
			}
		})
	}
}

// TestStatsLateCycleCardReplacesTheAwaitingMoreCyclesExplainer: on one or two
// completed cycles the late-cycle card says a personal range needs more
// completed cycles, and the explainer under it says fertile details wait for N
// of them — one fact twice. The card stays while the cycle is late; the
// explainer is the only place the floor is named while it is not. The
// dashboard, the calendar and the stats page render both from one context, so
// each is held to the same answer.
func TestStatsLateCycleCardReplacesTheAwaitingMoreCyclesExplainer(t *testing.T) {
	pages := []struct {
		path         string
		explainerTag string
		explainerKey string
	}{
		{path: "/stats", explainerTag: "data-stats-prediction-explainer", explainerKey: "data-explainer-key"},
		{path: "/calendar", explainerTag: "data-calendar-prediction-explainer", explainerKey: "data-explainer-primary-key"},
		{path: "/dashboard", explainerTag: "data-dashboard-prediction-explainer", explainerKey: "data-explainer-key"},
	}
	for index, testCase := range []struct {
		name          string
		cycleDay      int
		wantLateCard  bool
		wantExplainer bool
	}{
		{name: "late on two completed cycles", cycleDay: 31, wantLateCard: true, wantExplainer: false},
		{name: "not late on two completed cycles", cycleDay: 10, wantLateCard: false, wantExplainer: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user, authCookie, today := newStatsOverviewOwner(t, app, database, fmt.Sprintf("stats-late-explainer-%d@example.com", index))
			current := testCase.cycleDay - 1
			seedStatsOverviewCycleHistory(t, database, user, today, current+56, current+28, current)
			updateStatsOverviewUser(t, database, user, map[string]any{
				"last_period_start": services.AddCalendarDays(today, -current, time.UTC),
			})

			for _, page := range pages {
				document := fetchLateCycleNoticePage(t, app, authCookie, page.path)
				notice := findHTMLNodeWithAttr(document, "data-late-cycle-key")
				if (notice != nil) != testCase.wantLateCard {
					t.Fatalf("%s: late-cycle card present = %v, want %v on cycle day %d", page.path, notice != nil, testCase.wantLateCard, testCase.cycleDay)
				}
				if notice != nil && htmlAttr(notice, "data-late-cycle-key") != services.LateCycleNoPersonalRangeKey {
					t.Fatalf("%s: late card on two completed cycles = %q, want the no-personal-range variant", page.path, htmlAttr(notice, "data-late-cycle-key"))
				}
				explainer := findHTMLNodeWithAttr(document, page.explainerTag)
				if (explainer != nil) != testCase.wantExplainer {
					t.Fatalf("%s: awaiting-more-cycles explainer present = %v, want %v on cycle day %d", page.path, explainer != nil, testCase.wantExplainer, testCase.cycleDay)
				}
				if explainer != nil {
					if got := htmlAttr(explainer, page.explainerKey); got != awaitingMoreCyclesExplainerKey {
						t.Fatalf("%s: explainer key = %q, want %q", page.path, got, awaitingMoreCyclesExplainerKey)
					}
				}
			}
		})
	}
}

// TestEveryOwnerPageRendersTheExplainerWithTheFloorTheServiceResolved holds the
// dashboard, the calendar and the stats page to one rendering: a floor-naming
// explainer prints the floor in the plural form the floor selects, and one that
// names no number renders exactly as the catalogue wrote it.
func TestEveryOwnerPageRendersTheExplainerWithTheFloorTheServiceResolved(t *testing.T) {
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("building the locale manager: %v", err)
	}
	messages := manager.Messages(i18n.LangEN)
	floor := services.FertilityMinimumCycles()
	// The expected sentence is the catalogue's own value for the plural category
	// a floor of three selects in English, read by its key, with the number put
	// in: it does not go through the lookup the page uses. A floor of one would
	// select ".one", so the pinned category is refused rather than guessed.
	if floor == 1 {
		t.Fatalf("the floor is %d, which selects the .one form; this test pins the .other form and must name .one instead", floor)
	}
	otherForm, ok := messages[awaitingMoreCyclesExplainerKey+".other"]
	if !ok || !strings.Contains(otherForm, "%d") {
		t.Fatalf("the English .other form of %s is %q; want a sentence carrying %%d", awaitingMoreCyclesExplainerKey, otherForm)
	}
	counted := strings.Replace(otherForm, "%d", fmt.Sprint(floor), 1)

	pages := []struct {
		path         string
		explainerTag string
		keyAttribute string
	}{
		{path: "/stats", explainerTag: "data-stats-prediction-explainer", keyAttribute: "data-explainer-key"},
		{path: "/calendar", explainerTag: "data-calendar-prediction-explainer", keyAttribute: "data-explainer-primary-key"},
		{path: "/dashboard", explainerTag: "data-dashboard-prediction-explainer", keyAttribute: "data-explainer-key"},
	}
	for _, testCase := range []struct {
		name    string
		columns map[string]any
		wantKey string
		want    string
	}{
		{name: "floor-naming key", columns: map[string]any{}, wantKey: awaitingMoreCyclesExplainerKey, want: counted},
		{name: "key that names no number", columns: map[string]any{"unpredictable_cycle": true}, wantKey: "prediction.explainer.unpredictable", want: messages["prediction.explainer.unpredictable"]},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user, authCookie, today := newStatsOverviewOwner(t, app, database, "explainer-floor-"+strings.ReplaceAll(testCase.name, " ", "-")+"@example.com")
			seedStatsOverviewCycleHistory(t, database, user, today, 66, 38, 10)
			columns := map[string]any{"last_period_start": services.AddCalendarDays(today, -10, time.UTC)}
			for column, value := range testCase.columns {
				columns[column] = value
			}
			updateStatsOverviewUser(t, database, user, columns)

			for _, page := range pages {
				explainer := findHTMLNodeWithAttr(fetchLateCycleNoticePage(t, app, authCookie, page.path), page.explainerTag)
				if explainer == nil {
					t.Fatalf("%s renders no explainer", page.path)
				}
				if got := htmlAttr(explainer, page.keyAttribute); got != testCase.wantKey {
					t.Fatalf("%s explainer key = %q, want %q", page.path, got, testCase.wantKey)
				}
				if got := strings.TrimSpace(htmlNodeText(explainer)); !strings.Contains(got, testCase.want) {
					t.Fatalf("%s explainer text = %q, want it to contain %q", page.path, got, testCase.want)
				}
			}
		})
	}
}

// TestCycleFloorSentencesRenderTheFloorInEveryLanguage renders the stats empty
// state and the dashboard of an account on two completed cycles in each
// supported language. Each carries sentences that take the floor through %d; a
// catalogue value whose verb does not match the argument list renders as
// "%!(EXTRA ...)" or "%!d(MISSING)", and the floor itself must be printed.
func TestCycleFloorSentencesRenderTheFloorInEveryLanguage(t *testing.T) {
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("building the locale manager: %v", err)
	}
	floor := fmt.Sprint(services.FertilityMinimumCycles())

	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user, authCookie, today := newStatsOverviewOwner(t, app, database, "cycle-floor-languages@example.com")
	seedStatsOverviewCycleHistory(t, database, user, today, 66, 38, 10)
	updateStatsOverviewUser(t, database, user, map[string]any{
		"last_period_start": services.AddCalendarDays(today, -10, time.UTC),
	})

	for _, language := range manager.SupportedLanguages() {
		for _, path := range []string{"/stats", "/dashboard", "/calendar"} {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Accept-Language", language)
			request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=UTC"))
			request.Header.Set(timezoneHeaderName, "UTC")
			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusOK)
			body := mustReadBodyString(t, response.Body)

			if strings.Contains(body, "%!") {
				t.Errorf("%s in %s renders a format error: a floor sentence's verb does not match its argument list", path, language)
			}
			if path == "/stats" && !strings.Contains(body, floor) {
				t.Errorf("%s in %s never prints the floor %s", path, language, floor)
			}
		}
	}
}
