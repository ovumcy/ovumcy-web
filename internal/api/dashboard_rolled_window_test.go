package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/net/html"
	"gorm.io/gorm"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Once the running cycle's ovulation is behind today, GET /api/v1/stats/overview
// reads current_fertility and current_phase against the window it rolls forward.
// The dashboard header and the stats page's phase card used to keep reading them
// against the running cycle's window — "luteal, outside the estimated window"
// beside an API, and a calendar, that call today fertile. Each case renders both
// pages through the real handlers and templates for the same owner and day and
// holds them to the overview's answer and to the concrete values.
func TestDashboardAndStatsPageReadTodayAgainstTheRolledWindowLikeTheOverview(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		now           time.Time
		seed          func(t *testing.T, database *gorm.DB, user models.User, today time.Time)
		wantPhase     string
		wantFertility string
	}{
		{
			// Cycles of 24, 24, 24, 40 and 40 days, luteal phase 14, on cycle day
			// 20: the running ovulation (day 10) is behind today, the median of 24
			// rolls it to cycle day 34, and its window (days 29-34) is still ahead.
			// A window that already covers today only exists after the median
			// start is due, where the fertility half is withheld (WEB-378).
			name: "24-24-24-40-40 on cycle day 20",
			now:  time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC),
			seed: func(t *testing.T, database *gorm.DB, user models.User, today time.Time) {
				updateStatsOverviewUser(t, database, user, map[string]any{"luteal_phase": 14})
				seedStatsOverviewCycleHistory(t, database, user, today, 171, 147, 123, 99, 59, 19)
			},
			wantPhase:     "luteal",
			wantFertility: services.FertilityStatusOutsideEstimatedWindow,
		},
		{
			// Three 28-day cycles, the running one from 9999-12-10: its ovulation
			// (9999-12-23) is behind today and the one it rolls to falls past
			// 9999-12-31, so no window is published and nothing is read against one.
			name: "ovulation rolled past the year 9999",
			now:  time.Date(9999, time.December, 30, 12, 0, 0, 0, time.UTC),
			seed: func(t *testing.T, database *gorm.DB, user models.User, _ time.Time) {
				seedLateYear9999Cycles(t, database, user.ID, time.Date(9999, time.December, 10, 0, 0, 0, 0, time.UTC))
			},
			wantPhase:     "unknown",
			wantFertility: services.FertilityStatusUnknown,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			now := testCase.now
			app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
			user := createOnboardingTestUserAt(t, database, "rolled-window@example.com", "StrongPass1", true, now.AddDate(-1, 0, 0))
			today := services.DateAtLocation(now, time.UTC)
			testCase.seed(t, database, user, today)
			authCookie := issueAuthCookieForUser(t, user)

			_, payload := fetchStatsOverview(t, app, authCookie)
			if payload.Suppression.Predictions || payload.Suppression.Fertility {
				t.Fatalf("fixture: suppression = %+v, want the projection published", payload.Suppression)
			}
			if payload.CurrentPhase != testCase.wantPhase || payload.CurrentFertility != testCase.wantFertility {
				t.Fatalf("fixture: overview phase/fertility = %q/%q, want %q/%q", payload.CurrentPhase, payload.CurrentFertility, testCase.wantPhase, testCase.wantFertility)
			}

			header := findHTMLNodeWithAttr(fetchStatsOverviewDashboardDocument(t, app, authCookie), "data-dashboard-status-header")
			if header == nil {
				t.Fatal("the dashboard rendered no status header")
			}
			if rendered := htmlAttr(header, "data-dashboard-phase"); rendered != payload.CurrentPhase || rendered != testCase.wantPhase {
				t.Errorf("dashboard header phase = %q, overview current_phase = %q, want %q", rendered, payload.CurrentPhase, testCase.wantPhase)
			}
			if rendered := htmlAttr(header, "data-fertility-status"); rendered != payload.CurrentFertility || rendered != testCase.wantFertility {
				t.Errorf("dashboard header fertility = %q, overview current_fertility = %q, want %q", rendered, payload.CurrentFertility, testCase.wantFertility)
			}

			card := findHTMLNodeWithAttr(fetchRolledWindowStatsPageDocument(t, app, authCookie), "data-stats-current-phase")
			if card == nil {
				t.Fatal("the stats page rendered no current-phase card")
			}
			if rendered := htmlAttr(card, "data-stats-current-phase"); rendered != payload.CurrentPhase || rendered != testCase.wantPhase {
				t.Errorf("stats page phase = %q, overview current_phase = %q, want %q", rendered, payload.CurrentPhase, testCase.wantPhase)
			}
			if rendered := htmlAttr(card, "data-fertility-status"); rendered != payload.CurrentFertility || rendered != testCase.wantFertility {
				t.Errorf("stats page fertility = %q, overview current_fertility = %q, want %q", rendered, payload.CurrentFertility, testCase.wantFertility)
			}
		})
	}
}

// On a rolled day the hero header names the phase the overview publishes
// (MovedWindow.CurrentPhase), so the dashboard and the JSON overview speak of one
// phase, while the ribbon's day cells keep mapping the RUNNING cycle: the
// 24-24-24-40-40 history on cycle day 20 — rolled, the window still ahead —
// prints "luteal" in the header and on today's cell, which sits in the running
// cycle's luteal span. Which phase card is marked current follows the header, and
// is pinned beside the card breakdown
// (TestRolledWindowHeaderNamesTheOverviewPhaseAndMarksNoOtherCardCurrent).
func TestDashboardHeaderNamesTheOverviewPhaseWhileTheRibbonKeepsTheRunningCycle(t *testing.T) {
	now := time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
	user := createOnboardingTestUserAt(t, database, "rolled-window-ribbon@example.com", "StrongPass1", true, now.AddDate(-1, 0, 0))
	today := services.DateAtLocation(now, time.UTC)
	updateStatsOverviewUser(t, database, user, map[string]any{"luteal_phase": 14})
	seedStatsOverviewCycleHistory(t, database, user, today, 171, 147, 123, 99, 59, 19)
	authCookie := issueAuthCookieForUser(t, user)

	_, payload := fetchStatsOverview(t, app, authCookie)
	if payload.CurrentPhase != "luteal" {
		t.Fatalf("fixture: overview current_phase = %q, want %q on a rolled day", payload.CurrentPhase, "luteal")
	}

	document := fetchStatsOverviewDashboardDocument(t, app, authCookie)
	header := findHTMLNodeWithAttr(document, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("the dashboard rendered no status header")
	}
	if rendered := htmlAttr(header, "data-dashboard-phase"); rendered != payload.CurrentPhase {
		t.Errorf("dashboard header phase = %q, overview current_phase = %q, want one phase on both", rendered, payload.CurrentPhase)
	}

	var todayCell *html.Node
	for _, cell := range collectHTMLNodesWithAttr(document, "data-cycle-ribbon-day") {
		if htmlAttr(cell, "data-today") == "true" {
			todayCell = cell
		}
	}
	if todayCell == nil {
		t.Fatal("the dashboard ribbon rendered no cell for today")
	}
	if day := htmlAttr(todayCell, "data-cycle-ribbon-day"); day != "20" {
		t.Errorf("today's ribbon cell is day %q, want 20", day)
	}
	if rendered := htmlAttr(todayCell, "data-phase"); rendered != "luteal" {
		t.Errorf("today's ribbon cell phase = %q, want %q: the ribbon maps the running cycle", rendered, "luteal")
	}
}

func fetchRolledWindowStatsPageDocument(t *testing.T, app *fiber.App, authCookie string) *html.Node {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=UTC"))
	request.Header.Set(timezoneHeaderName, "UTC")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
}
