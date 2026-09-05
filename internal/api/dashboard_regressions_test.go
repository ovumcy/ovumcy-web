package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

func TestDashboardAndCalendarExposeAccessibleBBTInputs(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "bbt-accessibility@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"track_bbt":        true,
		"temperature_unit": services.TemperatureUnitCelsius,
	}).Error; err != nil {
		t.Fatalf("enable BBT tracking: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)
	dashboardResponse := mustAppResponse(t, app, dashboardRequest)
	assertStatusCode(t, dashboardResponse, http.StatusOK)

	dashboardBody := mustReadBodyString(t, dashboardResponse.Body)
	for _, fragment := range []string{
		`id="dashboard-bbt"`,
		`aria-labelledby="dashboard-bbt-legend"`,
		`aria-describedby="dashboard-bbt-hint"`,
	} {
		if !strings.Contains(dashboardBody, fragment) {
			t.Fatalf("expected dashboard BBT field markup %q", fragment)
		}
	}

	dayActionPrefix := `hx-post="/api/v1/days/`
	startIndex := strings.Index(dashboardBody, dayActionPrefix)
	if startIndex < 0 {
		t.Fatal("expected dashboard day form action")
	}
	dayStart := startIndex + len(dayActionPrefix)
	dayEnd := dayStart + len("2006-01-02")
	if len(dashboardBody) < dayEnd {
		t.Fatal("expected dashboard day form date to be present")
	}
	dayRaw := dashboardBody[dayStart:dayEnd]

	panelRequest := httptest.NewRequest(http.MethodGet, "/calendar/day/"+dayRaw+"?mode=edit", nil)
	panelRequest.Header.Set("Accept-Language", "en")
	panelRequest.Header.Set("Cookie", authCookie)
	panelResponse := mustAppResponse(t, app, panelRequest)
	assertStatusCode(t, panelResponse, http.StatusOK)

	panelBody := mustReadBodyString(t, panelResponse.Body)
	for _, fragment := range []string{
		`id="calendar-bbt"`,
		`aria-labelledby="calendar-bbt-legend"`,
		`aria-describedby="calendar-bbt-hint"`,
	} {
		if !strings.Contains(panelBody, fragment) {
			t.Fatalf("expected calendar BBT field markup %q", fragment)
		}
	}
}

func TestDashboardStaleCycleWarningIncludesSettingsCTAAndEstimatedPhase(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-stale-ui@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// Cycle day 31 against a 28-day reference: past the reference (stale) but
	// inside the seven-day grace window, so the late-cycle notice — which now
	// outranks the stale hint, see
	// TestDashboardLateCycleNoticeOutranksTheStaleHintAndClaimsNoInventedRange —
	// stays silent and this test still observes the state it names.
	lastPeriodStart := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -30)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}

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

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	warnings := dashboardElementByDataAttr(document, "data-dashboard-cycle-warnings")
	if warnings == nil {
		t.Fatal("expected dashboard cycle warning container when baseline is stale")
	}
	if dashboardElementByDataAttr(warnings, "data-dashboard-stale-warning") == nil {
		t.Fatal("expected stale cycle warning element inside the warning container")
	}
	settingsCTA := htmlFindElement(warnings, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && htmlAttr(node, "href") == "/settings#settings-cycle"
	})
	if settingsCTA == nil {
		t.Fatal("expected stale cycle warning to include direct settings CTA")
	}

	header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("expected the dashboard status header while cycle data is stale")
	}
	if got := htmlAttr(header, "data-dashboard-phase"); got != "unknown" {
		t.Fatalf("expected dashboard status header phase %q while cycle data is stale, got %q", "unknown", got)
	}
}

func TestDashboardAndStatsUseSameStalePhasePresentation(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-stats-stale-phase@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	lastPeriodStart := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -60)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update stale baseline for user: %v", err)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)
	dashboardResponse, err := app.Test(dashboardRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = dashboardResponse.Body.Close() }()

	dashboardDocument := mustParseHTMLDocument(t, mustReadBodyString(t, dashboardResponse.Body))
	dashboardHeader := dashboardElementByDataAttr(dashboardDocument, "data-dashboard-status-header")
	if dashboardHeader == nil {
		t.Fatal("expected the dashboard status header while cycle data is stale")
	}
	if got := htmlAttr(dashboardHeader, "data-dashboard-phase"); got != "unknown" {
		t.Fatalf("expected dashboard status header phase %q while cycle data is stale, got %q", "unknown", got)
	}
	if got := htmlAttr(dashboardHeader, "data-fertility-status"); got != "unknown" {
		t.Fatalf("expected dashboard status header fertility %q while cycle data is stale, got %q", "unknown", got)
	}

	statsRequest := httptest.NewRequest(http.MethodGet, "/stats", nil)
	statsRequest.Header.Set("Accept-Language", "en")
	statsRequest.Header.Set("Cookie", authCookie)
	statsResponse, err := app.Test(statsRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer func() { _ = statsResponse.Body.Close() }()

	statsDocument := mustParseHTMLDocument(t, mustReadBodyString(t, statsResponse.Body))
	if dashboardElementByDataAttr(statsDocument, "data-stats-empty-state") == nil {
		t.Fatal("expected stats page to show gated empty state before enough completed cycles")
	}
}

// dashboardLateCycleNotice returns the late-cycle paragraph and the row of
// logging actions rendered beside it, so a test can address the chosen state by
// its data-late-cycle-key attribute rather than by the rendered phrase.
func dashboardLateCycleNotice(t *testing.T, app *fiber.App, authCookie string) (*html.Node, *html.Node) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	warnings := dashboardElementByDataAttr(document, "data-dashboard-cycle-warnings")
	if warnings == nil {
		t.Fatal("expected the dashboard cycle warning container on a cycle past its expected end")
	}
	if dashboardElementByDataAttr(warnings, "data-dashboard-stale-warning") != nil {
		t.Fatal("expected the late-cycle notice to replace the stale hint, not to render beside it")
	}
	notice := dashboardElementByDataAttr(warnings, "data-dashboard-cycle-day-warning")
	if notice == nil {
		t.Fatal("expected the late-cycle notice inside the warning container")
	}
	return notice, dashboardElementByDataAttr(warnings, "data-dashboard-late-cycle-actions")
}

// TestDashboardLateCycleNoticeOutranksTheStaleHintAndClaimsNoInventedRange is
// the design-item-38 render regression for the insufficient-history half of the
// late-cycle matrix. An account whose only cycle input is the onboarding
// baseline has no completed cycle to compare against, so the notice must select
// the no-personal-range key: the "usual range" it would otherwise cite is the
// settings value, not a measurement.
func TestDashboardLateCycleNoticeOutranksTheStaleHintAndClaimsNoInventedRange(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-late-cycle-no-history@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	lastPeriodStart := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -60)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}

	notice, actions := dashboardLateCycleNotice(t, app, authCookie)
	if got := htmlAttr(notice, "data-late-cycle-key"); got != services.LateCycleNoPersonalRangeKey {
		t.Fatalf("expected late-cycle key %q with no completed cycles, got %q", services.LateCycleNoPersonalRangeKey, got)
	}
	if got := htmlAttr(notice, "data-late-cycle-tone"); got != services.LateCycleToneNeutral {
		t.Fatalf("expected a neutral tone when no range can be measured, got %q", got)
	}

	if actions == nil {
		t.Fatal("expected the late-cycle notice to offer the logging actions that fit")
	}
	for _, action := range []string{"cycle-start", "pregnancy-test", "symptoms"} {
		link := htmlFindElement(actions, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "data-late-cycle-action") == action
		})
		if link == nil {
			t.Fatalf("expected a %q logging action beside the late-cycle notice", action)
		}
		target := strings.TrimPrefix(htmlAttr(link, "href"), "#")
		if target == "" {
			t.Fatalf("expected the %q action to link to a control on the page", action)
		}
	}
}

// TestDashboardLateCycleNoticeStatesTheMeasuredExcessOnceHistoryExists is the
// paired positive: the same surface, the same hooks, but an account that owns
// two completed cycles — which is exactly the threshold the stats
// prediction-reliability card uses — so the notice may state a measured excess.
func TestDashboardLateCycleNoticeStatesTheMeasuredExcessOnceHistoryExists(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-late-cycle-history@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":  28,
		"period_length": 5,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}
	// Two completed 28-day cycles, then a running cycle on day 41 — past the
	// 28-day reference plus the seven-day grace window, and past the observed
	// maximum of 28 days by 13.
	for _, offsetDays := range []int{-96, -68, -40} {
		start := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, offsetDays)
		if err := database.Create(&models.DailyLog{
			UserID:     user.ID,
			Date:       start,
			IsPeriod:   true,
			CycleStart: true,
		}).Error; err != nil {
			t.Fatalf("seed cycle start %d: %v", offsetDays, err)
		}
	}

	notice, actions := dashboardLateCycleNotice(t, app, authCookie)
	if got := htmlAttr(notice, "data-late-cycle-key"); got != services.LateCycleBeyondRangeKey {
		t.Fatalf("expected late-cycle key %q once a personal range exists, got %q", services.LateCycleBeyondRangeKey, got)
	}
	if got := htmlAttr(notice, "data-late-cycle-tone"); got != services.LateCycleToneWarning {
		t.Fatalf("expected the measured-excess state to carry the warning tone, got %q", got)
	}
	if actions == nil {
		t.Fatal("expected the late-cycle notice to offer the logging actions that fit")
	}
}

// TestDashboardAndStatsAgreeOnPhaseAndFertilityOnAFertileWindowDay is the
// item-24 render regression: on a fertile-window day both primary surfaces
// declare the same 4-value phase, and "fertile" appears only as the separate
// fertility-status axis — never in the phase slot.
func TestDashboardAndStatsAgreeOnPhaseAndFertilityOnAFertileWindowDay(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-fertile-window-day@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// Two completed 28-day cycles unlock the stats KPI row; cycle 28 /
	// luteal 14 → ovulation day 14, fertile window days 9–14. Day 12 sits
	// inside the window with a ±1-day timezone slack on both sides.
	lastPeriodStart := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -11)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}
	for _, offsetDays := range []int{-67, -39, -11} {
		start := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, offsetDays)
		if err := database.Create(&models.DailyLog{
			UserID:     user.ID,
			Date:       start,
			IsPeriod:   true,
			CycleStart: true,
		}).Error; err != nil {
			t.Fatalf("seed cycle start %d: %v", offsetDays, err)
		}
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)
	dashboardResponse, err := app.Test(dashboardRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = dashboardResponse.Body.Close() }()

	dashboardDocument := mustParseHTMLDocument(t, mustReadBodyString(t, dashboardResponse.Body))
	header := dashboardElementByDataAttr(dashboardDocument, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("expected the status header on a fresh predictable baseline")
	}
	if got := htmlAttr(header, "data-dashboard-phase"); got != "follicular" {
		t.Fatalf("expected status header phase %q on a fertile-window day, got %q", "follicular", got)
	}
	if got := htmlAttr(header, "data-fertility-status"); got != "fertile" {
		t.Fatalf("expected status header fertility status %q on a fertile-window day, got %q", "fertile", got)
	}

	statsRequest := httptest.NewRequest(http.MethodGet, "/stats", nil)
	statsRequest.Header.Set("Accept-Language", "en")
	statsRequest.Header.Set("Cookie", authCookie)
	statsResponse, err := app.Test(statsRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer func() { _ = statsResponse.Body.Close() }()

	statsDocument := mustParseHTMLDocument(t, mustReadBodyString(t, statsResponse.Body))
	phaseValue := dashboardElementByDataAttr(statsDocument, "data-stats-current-phase")
	if phaseValue == nil {
		t.Fatal("expected the stats current-phase value to declare its phase hook")
	}
	if got := htmlAttr(phaseValue, "data-stats-current-phase"); got != "follicular" {
		t.Fatalf("expected stats phase %q on a fertile-window day, got %q", "follicular", got)
	}
	if got := htmlAttr(phaseValue, "data-fertility-status"); got != "fertile" {
		t.Fatalf("expected stats fertility status %q on a fertile-window day, got %q", "fertile", got)
	}
	if dashboardElementByDataAttr(statsDocument, "data-fertile-window") == nil {
		t.Fatal("expected the stats phase card to render the fertile-window line on a fertile day")
	}
}

// TestStatsFertileWindowCardIsSuppressedByAPregnancyPause is the render
// regression for the one prediction surface the pregnancy pause did not reach.
// /stats resolved the flag its phase card gates on from
// DashboardPredictionDisabled — the owner's unpredictable-cycle setting alone —
// while the calendar grid, the .ics feed and the webhook pass all gate on the
// shared suppression predicate, so an owner whose latest fertility signal is a
// positive pregnancy test still read "Fertile window" on /stats: a claim
// projected from the onboarding cycle-length slider, which the medical-safety
// invariant says must be suppressed rather than captioned.
//
// Both accounts sit on day 12 of a 28-day baseline with two completed cycles
// behind them — the tier the card lives in (HasInsights, >= 2) and a
// fertile-window day by the same math the test above uses — and differ only in
// the positive pregnancy test. The unpaused case is the positive anchor for the
// paused one: it proves the two hooks asserted absent under the pause are the
// hooks this surface actually renders, so the suppression case cannot pass by
// naming an attribute that no longer exists.
//
// Deliberately narrower than its subject: it pins the pregnancy pause only. The
// zero-completed-cycle tier never reaches this card (HasInsights excludes it)
// and an overdue cycle is already caught by the stronger CycleDataStale branch
// above the card's fertility line.
func TestStatsFertileWindowCardIsSuppressedByAPregnancyPause(t *testing.T) {
	for name, testCase := range map[string]struct {
		account         string
		pregnancyPaused bool
	}{
		"no pregnancy test: the fertile-window claim renders": {account: "unpaused"},
		"positive pregnancy test: the claim is withheld":      {account: "paused", pregnancyPaused: true},
	} {
		t.Run(name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "stats-pregnancy-pause-"+testCase.account+"@example.com", "StrongPass1", true)
			today := services.DateAtLocation(time.Now().UTC(), time.UTC)
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
				"cycle_length":      28,
				"period_length":     5,
				"last_period_start": today.AddDate(0, 0, -11),
			}).Error; err != nil {
				t.Fatalf("seed cycle baseline: %v", err)
			}
			// Three recorded starts 28 days apart: two completed cycles, and the
			// running one on day 12 — inside the fertile window (ovulation day 14,
			// window days 9-14) with a day of timezone slack on either side.
			for _, offsetDays := range []int{-67, -39, -11} {
				if err := database.Create(&models.DailyLog{
					UserID:     user.ID,
					Date:       today.AddDate(0, 0, offsetDays),
					IsPeriod:   true,
					CycleStart: true,
				}).Error; err != nil {
					t.Fatalf("seed cycle start %d: %v", offsetDays, err)
				}
			}
			if testCase.pregnancyPaused {
				// Positive test after the running cycle's start, with no later cycle
				// start: ResolvePregnancyPause reports an active pause.
				if err := database.Create(&models.DailyLog{
					UserID:        user.ID,
					Date:          today.AddDate(0, 0, -2),
					PregnancyTest: models.PregnancyTestPositive,
				}).Error; err != nil {
					t.Fatalf("seed positive pregnancy test: %v", err)
				}
			}

			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			request := httptest.NewRequest(http.MethodGet, "/stats", nil)
			request.Header.Set("Accept-Language", "en")
			request.Header.Set("Cookie", authCookie)
			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("stats request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
			fertilityStatus := ""
			if statusValue := dashboardElementByDataAttr(document, "data-fertility-status"); statusValue != nil {
				fertilityStatus = htmlAttr(statusValue, "data-fertility-status")
			}
			fertileWindowLine := dashboardElementByDataAttr(document, "data-fertile-window")

			if !testCase.pregnancyPaused {
				if fertilityStatus != "fertile" {
					t.Errorf("anchor: expected /stats to declare fertility %q on a fertile-window day, got %q", "fertile", fertilityStatus)
				}
				if fertileWindowLine == nil {
					t.Error("anchor: expected /stats to render the fertile-window line on a fertile-window day")
				}
				return
			}
			if fertilityStatus == "fertile" {
				t.Errorf("expected /stats to withhold the fertility status under a pregnancy pause, got data-fertility-status=%q", fertilityStatus)
			}
			if fertileWindowLine != nil {
				t.Error("expected /stats to withhold the fertile-window line under a pregnancy pause, got data-fertile-window rendered")
			}
		})
	}
}

// TestDashboardHeaderWithholdsFertilityUntilTheFirstCompletedCycle is the
// render regression for the first reliability tier. Both accounts below sit on
// cycle day 12 of a 28-day baseline — a fertile-window day by the same math the
// test above uses — and differ only in whether one cycle has been observed.
//
// With none, the fertile window and the ovulation date exist only because the
// onboarding cycle-length slider was projected forward, so the header shows
// neither and declares no fertility status; an account tracking to conceive
// reads one bridge line naming when the window arrives instead. With one
// completed cycle the header renders exactly as it always did. What the tier
// never touches is asserted in both states: the phase and the next-period item,
// which carries its own estimate qualifier.
func TestDashboardHeaderWithholdsFertilityUntilTheFirstCompletedCycle(t *testing.T) {
	for name, testCase := range map[string]struct {
		account        string
		goal           string
		completedCycle bool
		wantFertility  bool
		wantOvulation  bool
		wantBridge     bool
	}{
		"trying to conceive, fresh baseline": {
			account:    "trying-fresh",
			goal:       models.UsageGoalTrying,
			wantBridge: true,
		},
		"trying to conceive, one completed cycle": {
			account:        "trying-one-cycle",
			goal:           models.UsageGoalTrying,
			completedCycle: true,
			wantFertility:  true,
			wantOvulation:  true,
		},
		"general health, fresh baseline": {
			account: "health-fresh",
			goal:    models.UsageGoalHealth,
		},
		"general health, one completed cycle": {
			account:        "health-one-cycle",
			goal:           models.UsageGoalHealth,
			completedCycle: true,
			wantFertility:  true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "dashboard-first-tier-"+testCase.account+"@example.com", "StrongPass1", true)
			today := services.DateAtLocation(time.Now().UTC(), time.UTC)
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
				"usage_goal":        testCase.goal,
				"cycle_length":      28,
				"period_length":     5,
				"last_period_start": today.AddDate(0, 0, -11),
			}).Error; err != nil {
				t.Fatalf("seed cycle baseline: %v", err)
			}
			if testCase.completedCycle {
				// Two recorded starts 28 days apart: one completed cycle, and the
				// running one still on day 12.
				for _, offsetDays := range []int{-39, -11} {
					if err := database.Create(&models.DailyLog{
						UserID:     user.ID,
						Date:       today.AddDate(0, 0, offsetDays),
						IsPeriod:   true,
						CycleStart: true,
					}).Error; err != nil {
						t.Fatalf("seed cycle start %d: %v", offsetDays, err)
					}
				}
			}

			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))

			header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
			if header == nil {
				t.Fatal("expected the dashboard status header")
			}
			wantStatus := "unknown"
			if testCase.wantFertility {
				wantStatus = "fertile"
			}
			if got := htmlAttr(header, "data-fertility-status"); got != wantStatus {
				t.Fatalf("expected the header to declare fertility %q, got %q", wantStatus, got)
			}

			statusLine := dashboardElementByDataAttr(document, "data-dashboard-status-line")
			if statusLine == nil {
				t.Fatal("expected the dashboard status line")
			}
			if got := htmlFindElement(statusLine, htmlNodeHasAttr("data-fertile-window")) != nil; got != testCase.wantFertility {
				t.Fatalf("expected the fertile-window item=%v, got %v", testCase.wantFertility, got)
			}
			if got := htmlFindElement(statusLine, htmlNodeHasAttr("data-dashboard-ovulation")) != nil; got != testCase.wantOvulation {
				t.Fatalf("expected the ovulation estimate=%v, got %v", testCase.wantOvulation, got)
			}

			bridge := htmlFindElement(statusLine, htmlNodeHasAttr("data-dashboard-first-cycle-bridge"))
			if got := bridge != nil; got != testCase.wantBridge {
				t.Fatalf("expected the first-cycle bridge line=%v, got %v", testCase.wantBridge, got)
			}
			if bridge != nil {
				if got := htmlAttr(bridge, "data-first-cycle-bridge-key"); got != "dashboard.fertile_window_after_first_cycle" {
					t.Fatalf("expected the bridge line to name its copy key, got %q", got)
				}
			}

			// The tier withholds the two slider-derived items and nothing else.
			if got := htmlAttr(header, "data-dashboard-phase"); got == "" || got == "unknown" {
				t.Fatalf("expected the phase to survive the tier, got %q", got)
			}
			if htmlFindElement(statusLine, htmlNodeHasAttr("data-dashboard-next-period")) == nil {
				t.Fatal("expected the next-period estimate to survive the tier")
			}
		})
	}
}

func TestDashboardTodaySavePersistsPeriodToggleAndNotes(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-today-save@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	todayRaw := today.Format("2006-01-02")
	note := "Remember hydration and rest"

	form := url.Values{
		"is_period": {"true"},
		"flow":      {models.FlowNone},
		"notes":     {note},
	}
	saveResponse := mustAppResponse(t, app, dashboardSaveRequest(todayRaw, form, authCookie))
	assertStatusCode(t, saveResponse, http.StatusOK)

	saveBody := mustReadBodyString(t, saveResponse.Body)
	if !strings.Contains(saveBody, "status-ok") {
		t.Fatalf("expected save status success markup")
	}

	parsedDay, err := services.ParseDayDate(todayRaw, time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, parsedDay, time.UTC)
	if err != nil {
		t.Fatalf("load stored day after dashboard save: %v", err)
	}
	if !entry.IsPeriod {
		t.Fatal("expected period toggle to persist after dashboard save")
	}
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected flow to remain %q, got %q", models.FlowNone, entry.Flow)
	}
	if entry.Notes != note {
		t.Fatalf("expected notes %q, got %q", note, entry.Notes)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)
	dashboardResponse := mustAppResponse(t, app, dashboardRequest)
	assertStatusCode(t, dashboardResponse, http.StatusOK)

	rendered := mustReadBodyString(t, dashboardResponse.Body)
	periodCheckedPattern := regexp.MustCompile(`(?s)name="is_period"[^>]*checked`)
	if !periodCheckedPattern.MatchString(rendered) {
		t.Fatalf("expected dashboard period toggle to remain checked after reload")
	}
	if !strings.Contains(rendered, note) {
		t.Fatalf("expected dashboard notes field to include saved note %q", note)
	}
}

func dashboardSaveRequest(todayRaw string, form url.Values, authCookie string) *http.Request {
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+todayRaw, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	return request
}

func TestDashboardTodaySavePersistsAndRendersWithNonUTCTimezone(t *testing.T) {
	app, database, location := newOnboardingTestAppWithLocation(t, time.FixedZone("UTC+3", 3*60*60))
	user := createOnboardingTestUser(t, database, "dashboard-today-tz@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(location), location).Format("2006-01-02")
	note := "timezone save note"

	form := url.Values{
		"is_period": {"true"},
		"flow":      {"none"},
		"notes":     {note},
	}

	saveRequest := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+today, strings.NewReader(form.Encode()))
	saveRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveRequest.Header.Set("HX-Request", "true")
	saveRequest.Header.Set("Accept-Language", "en")
	saveRequest.Header.Set("Cookie", authCookie)

	saveResponse, err := app.Test(saveRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("save request failed: %v", err)
	}
	defer func() { _ = saveResponse.Body.Close() }()

	if saveResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", saveResponse.StatusCode)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)

	dashboardResponse, err := app.Test(dashboardRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = dashboardResponse.Body.Close() }()

	if dashboardResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", dashboardResponse.StatusCode)
	}

	body, err := io.ReadAll(dashboardResponse.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	rendered := string(body)

	periodCheckedPattern := regexp.MustCompile(`(?s)name="is_period"[^>]*checked`)
	if !periodCheckedPattern.MatchString(rendered) {
		t.Fatal("expected period toggle to remain checked for saved day in non-UTC timezone")
	}
	if !strings.Contains(rendered, note) {
		t.Fatalf("expected notes to be restored in dashboard textarea, got body without %q", note)
	}
}

// TestDashboardRendersReminderBannerWhenPeriodIsDueSoon covers issue #123:
// once the existing next-period prediction falls inside the reminder
// window (here 2 days out, the "~N days" plural branch), the dashboard must
// render the banner with its plural day-count copy, and the always-on
// medical-safety disclaimer must still be present immediately alongside it.
func TestDashboardRendersReminderBannerWhenPeriodIsDueSoon(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-reminder-due@example.com", "StrongPass1", true)

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	// A 28-day cycle started 26 days ago predicts the next period in 2
	// days — inside the default reminder window.
	lastPeriodStart := today.AddDate(0, 0, -26)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update due-soon cycle context: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	body := mustReadBodyString(t, response.Body)
	document := mustParseHTMLDocument(t, body)

	banner := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-dashboard-reminder-banner")
	})
	if banner == nil {
		t.Fatal("expected dashboard reminder banner to render when the next period is due soon")
	}
	if got := htmlAttr(banner, "data-reminder-banner-key"); got != "dashboard.reminder_banner_period" {
		t.Fatalf("expected period reminder banner key, got %q", got)
	}

	for _, fragment := range []string{
		`data-dashboard-prediction-disclaimer`,
		"not medical advice or a method of contraception",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("expected the medical-safety disclaimer fragment %q to render alongside the reminder banner", fragment)
		}
	}
}

// TestDashboardRendersTomorrowReminderBannerCopy pins the day-1 branch:
// a next-period prediction exactly one day out must select the dedicated
// "tomorrow" copy (a non-plural i18n key), surfaced through the stable
// data-reminder-banner-key hook rather than the "~N days" plural.
func TestDashboardRendersTomorrowReminderBannerCopy(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-reminder-tomorrow@example.com", "StrongPass1", true)

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	// A 28-day cycle started 27 days ago predicts the next period tomorrow
	// (1 day out) — the dedicated "tomorrow" copy branch.
	lastPeriodStart := today.AddDate(0, 0, -27)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update tomorrow cycle context: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	body := mustReadBodyString(t, response.Body)
	document := mustParseHTMLDocument(t, body)

	banner := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-dashboard-reminder-banner")
	})
	if banner == nil {
		t.Fatal("expected dashboard reminder banner to render when the next period is due tomorrow")
	}
	if got := htmlAttr(banner, "data-reminder-banner-key"); got != "dashboard.reminder_banner_period_tomorrow" {
		t.Fatalf("expected period tomorrow reminder banner key, got %q", got)
	}

	for _, fragment := range []string{
		`data-dashboard-prediction-disclaimer`,
		"not medical advice or a method of contraception",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("expected the medical-safety disclaimer fragment %q to render alongside the reminder banner", fragment)
		}
	}
}

// TestDashboardOmitsReminderBannerWhenPeriodIsNotYetDueSoon is the negative
// counterpart of TestDashboardRendersReminderBannerWhenPeriodIsDueSoon: a
// prediction far outside the reminder window must not render the banner,
// while every other dashboard prediction surface (and its disclaimer) keeps
// working exactly as before.
func TestDashboardOmitsReminderBannerWhenPeriodIsNotYetDueSoon(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-reminder-not-due@example.com", "StrongPass1", true)

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	// A 28-day cycle started 2 days ago predicts the next period in 26
	// days — far outside the default reminder window.
	lastPeriodStart := today.AddDate(0, 0, -2)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update not-due cycle context: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	if htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-dashboard-reminder-banner")
	}) != nil {
		t.Fatal("did not expect a reminder banner when the prediction is far outside the window")
	}
}

// TestBuildDashboardViewDataOmitsReminderBannerForNonOwner exercises the
// view-data builder directly (bypassing HTTP/session plumbing) to pin that a
// non-owner never receives reminder-banner fields, matching how every other
// owner-only prediction field on the dashboard behaves.
func TestBuildDashboardViewDataOmitsReminderBannerForNonOwner(t *testing.T) {
	handler, database := newDataAccessTestHandler(t)
	user := createDataAccessTestUser(t, database, "dashboard-reminder-non-owner@example.com")
	user.Role = "viewer"

	now := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)
	data, err := handler.buildDashboardViewData(context.Background(), &user, "en", map[string]string{}, now, time.UTC)
	if err != nil {
		t.Fatalf("build dashboard view data: %v", err)
	}

	if show, ok := data["ShowReminderBanner"].(bool); !ok || show {
		t.Fatalf("expected ShowReminderBanner=false for a non-owner, got %v (ok=%v)", show, ok)
	}
}

// TestDashboardDoesNotFlagTodaysOvulationAsPastWestOfUTC is the rendered half of
// the services-layer guard in calendar_day_comparison_regression_test.go: the
// projected ovulation date is a UTC-midnight value while the request's "today"
// is a location midnight, so west of UTC the ovulation day itself read as
// already past and the amber prediction-past notice rendered beside the very
// date the header had just named. One ordinary account, no irregular mode, no
// DST date — the state appeared on the ovulation day of every cycle.
//
// The anchor is seeded relative to the live clock: the subject computes against
// time.Now(), so a fixed calendar date would drift out of the cycle it was
// written for.
func TestDashboardDoesNotFlagTodaysOvulationAsPastWestOfUTC(t *testing.T) {
	const timezoneName = "America/New_York"

	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		t.Fatalf("load %s: %v", timezoneName, err)
	}

	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-ovulation-today@example.com", "StrongPass1", true)

	// A 28-day cycle with the default 14-day luteal phase ovulates on cycle
	// day 14, so an anchor 13 days back puts the predicted ovulation on today.
	today := services.DateAtLocation(time.Now(), location)
	currentStart := today.AddDate(0, 0, -13)
	previousStart := currentStart.AddDate(0, 0, -28)
	for _, cycleStart := range []time.Time{previousStart, currentStart} {
		for offset := range 5 {
			day := services.CalendarDay(cycleStart.AddDate(0, 0, offset), time.UTC)
			entry := models.DailyLog{
				UserID:     user.ID,
				Date:       day,
				IsPeriod:   true,
				CycleStart: offset == 0,
				Flow:       models.FlowMedium,
			}
			if err := database.Create(&entry).Error; err != nil {
				t.Fatalf("seed period day %s: %v", day.Format("2006-01-02"), err)
			}
		}
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": services.CalendarDay(currentStart, time.UTC),
	}).Error; err != nil {
		t.Fatalf("update ovulation-today cycle context: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	response := dashboardWithTimezoneResponse(t, app, authCookie, timezoneName)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	if htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-dashboard-prediction-past")
	}) != nil {
		t.Fatal("did not expect the prediction-past notice on the ovulation day itself")
	}
}

func newOnboardingTestAppWithLocation(t *testing.T, location *time.Location) (*fiber.App, *gorm.DB, *time.Location) {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "ovumcy-onboarding-test-tz.db")

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}

	handler, err := NewHandler("test-secret-key", location, i18nManager, false, newTestHandlerDependencies(database, i18nManager))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}

	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	RegisterRoutes(app, handler)
	return app, database, location
}
