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
// explainer is the only place the floor is named while it is not.
func TestStatsLateCycleCardReplacesTheAwaitingMoreCyclesExplainer(t *testing.T) {
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

			document := fetchLateCycleNoticePage(t, app, authCookie, "/stats")
			lateCard := findHTMLNodeWithAttr(document, "data-stats-late-cycle")
			if (lateCard != nil) != testCase.wantLateCard {
				t.Fatalf("late-cycle card present = %v, want %v on cycle day %d", lateCard != nil, testCase.wantLateCard, testCase.cycleDay)
			}
			if lateCard != nil {
				notice := findHTMLNodeWithAttr(document, "data-late-cycle-key")
				if notice == nil || htmlAttr(notice, "data-late-cycle-key") != services.LateCycleNoPersonalRangeKey {
					t.Fatalf("late card on two completed cycles must be the no-personal-range variant, got %v", notice)
				}
			}
			explainer := findHTMLNodeWithAttr(document, "data-stats-prediction-explainer")
			if (explainer != nil) != testCase.wantExplainer {
				t.Fatalf("awaiting-more-cycles explainer present = %v, want %v on cycle day %d", explainer != nil, testCase.wantExplainer, testCase.cycleDay)
			}
			if explainer != nil {
				if got := htmlAttr(explainer, "data-explainer-key"); got != awaitingMoreCyclesExplainerKey {
					t.Fatalf("explainer key = %q, want %q", got, awaitingMoreCyclesExplainerKey)
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
