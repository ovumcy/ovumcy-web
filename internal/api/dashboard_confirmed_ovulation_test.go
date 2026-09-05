package api

// dashboard_confirmed_ovulation_test.go — the RENDERED ovulation slot, for the
// one cohort whose divergence a context-level assertion cannot see.
//
// services.BuildDashboardCycleContext keeps DisplayOvulationDate once the
// thermal detector has confirmed a day, but dashboard.html tests
// DisplayOvulationNeedsData BEFORE the branch that names a date — so for an
// irregular account with one or two completed cycles the caption won and the
// date it carried was never reached, while the calendar grid (gated on
// FertilityProjectionSuppressed alone) marked the detector's day. A test that
// read the context alone was green through all of it.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// The two temperatures every 3-over-6 series in this package is built from:
// six undisturbed readings at the low value fill the coverline window, three at
// the high value clear it by more than the detector's 0.2 °C margin. Which
// cycle days carry them is each seed's own business.
const (
	dashboardConfirmedOvulationLowBBT  = 36.20
	dashboardConfirmedOvulationHighBBT = 36.50
)

// seedLateThermalShiftCycle records the late-shift cohort behind both halves of
// its regression (the JSON overview and the rendered dashboard slot): three
// prior 28-day cycles fix the median at 28 and the fourth start opens the
// CURRENT cycle at today-31 — cycle day 32, not overdue (32 <= 28+7) — so the
// model projects the next period on today-3, the very day the coverline
// window's sixth reading falls on. A detector bounded at that projection never
// saw the sixth reading and the shift went unseen; bounded at today+1 it does.
// Returns the day the detector confirms.
func seedLateThermalShiftCycle(t *testing.T, database *gorm.DB, user models.User, today time.Time) time.Time {
	t.Helper()

	seedStatsOverviewCycleHistory(t, database, user, today, 115, 87, 59, 31)
	logs := make([]models.DailyLog, 0, 9)
	for _, offset := range []int{8, 7, 6, 5, 4, 3} {
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: services.AddCalendarDays(today, -offset, time.UTC), BBT: new(dashboardConfirmedOvulationLowBBT)})
	}
	for _, offset := range []int{2, 1, 0} {
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: services.AddCalendarDays(today, -offset, time.UTC), BBT: new(dashboardConfirmedOvulationHighBBT)})
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("seed late thermal shift: %v", err)
	}
	return services.AddCalendarDays(today, -3, time.UTC)
}

// TestDashboardNamesALateShiftAfterTheProjectedNextPeriodStart is the rendered
// half of TestStatsOverviewConfirmsALateShiftAfterTheProjectedNextPeriodStart
// (stats_overview_contract_test.go): the same cohort read through the
// dashboard's ovulation slot rather than the JSON payload, so the two
// owner-facing surfaces cannot name different days for one late shift.
func TestDashboardNamesALateShiftAfterTheProjectedNextPeriodStart(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-late-shift@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	// The rendered ovulation slot exists only for an account tracking to
	// conceive (resolveDashboardTimingFrame, dashboard_view_service.go).
	updateStatsOverviewUser(t, database, user, map[string]any{"track_bbt": true, "usage_goal": models.UsageGoalTrying})
	confirmedDay := seedLateThermalShiftCycle(t, database, user, today)

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("fixture anchor: expected the dashboard status header")
	}
	ovulation := dashboardElementByDataAttr(header, "data-dashboard-ovulation")
	if ovulation == nil {
		t.Fatal("fixture anchor: expected the ovulation slot in the status line")
	}

	slot := normalizeHTMLText(htmlNodeText(ovulation))
	if want := services.LocalizedDateDisplay("en", confirmedDay); !strings.Contains(slot, want) {
		t.Fatalf("the ovulation slot = %q, want the BBT-confirmed %q — a shift recorded after the projected next period start is still this cycle's", slot, want)
	}
}

// TestDashboardNamesTheConfirmedDayForTheThinHistoryCohort builds the cohort
// that meets dashboardNeedsOvulationData (irregular, one completed cycle) while
// clearing the first-cycle fertility floor, records a thermal shift in the
// current cycle, and reads the rendered slot.
//
// Anchors first: the slot must exist at all (the trying-to-conceive goal and a
// cleared floor put it there), so a fixture that drifts out of the band fails
// loudly instead of passing on an absent surface.
func TestDashboardNamesTheConfirmedDayForTheThinHistoryCohort(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-confirmed-ovulation@example.com", "StrongPass1", true)

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	const cycleLength = 28
	const periodLength = 5
	// Cycle day 14 today, so the model projects ovulation on today itself: the
	// day the projection and the measurement disagree by three days.
	cycleStart := today.AddDate(0, 0, -13)
	previousStart := cycleStart.AddDate(0, 0, -cycleLength)
	confirmedDay := cycleStart.AddDate(0, 0, 10)

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      cycleLength,
		"period_length":     periodLength,
		"last_period_start": cycleStart,
		"usage_goal":        models.UsageGoalTrying,
		"irregular_cycle":   true,
		"track_bbt":         true,
	}).Error; err != nil {
		t.Fatalf("update confirmed-ovulation cycle context: %v", err)
	}

	// Two starts, so exactly one completed cycle: under three (the withholding
	// this test is about) and at least one (the floor that would withhold the
	// slot entirely).
	for _, start := range []time.Time{previousStart, cycleStart} {
		for offset := range periodLength {
			if err := database.Create(&models.DailyLog{
				UserID:     user.ID,
				Date:       start.AddDate(0, 0, offset),
				IsPeriod:   true,
				CycleStart: offset == 0,
				Flow:       models.FlowMedium,
			}).Error; err != nil {
				t.Fatalf("create period log %s day %d: %v", start.Format("2006-01-02"), offset, err)
			}
		}
	}
	for offset := 5; offset <= 13; offset++ {
		temperature := dashboardConfirmedOvulationLowBBT
		if offset > 10 {
			temperature = dashboardConfirmedOvulationHighBBT
		}
		value := temperature
		if err := database.Create(&models.DailyLog{
			UserID: user.ID,
			Date:   cycleStart.AddDate(0, 0, offset),
			BBT:    &value,
		}).Error; err != nil {
			t.Fatalf("create bbt log at cycle day %d: %v", offset+1, err)
		}
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("fixture anchor: expected the dashboard status header")
	}
	ovulation := dashboardElementByDataAttr(header, "data-dashboard-ovulation")
	if ovulation == nil {
		t.Fatal("fixture anchor: expected the ovulation slot in the status line")
	}

	slot := normalizeHTMLText(htmlNodeText(ovulation))
	if strings.Contains(slot, "completed cycles are needed") {
		t.Fatalf("the ovulation slot withheld a measured day behind the thin-history caption: %q", slot)
	}
	if want := services.LocalizedDateDisplay("en", confirmedDay); !strings.Contains(slot, want) {
		t.Fatalf("the ovulation slot = %q, want the detector's day %q", slot, want)
	}
	if projected := services.LocalizedDateDisplay("en", today); strings.Contains(slot, projected) {
		t.Fatalf("the ovulation slot still names the projected day %q: %q", projected, slot)
	}

	// The amber notice is about a projection the model still points at after the
	// day has gone by. A measured ovulation is behind the owner for the whole
	// luteal phase by design, so reading it as that notice would leave the
	// warning standing for a fortnight of every cycle. The next period is
	// fifteen days out in this fixture, so its own in-past branch cannot be what
	// keeps this quiet.
	if dashboardElementByDataAttr(document, "data-dashboard-prediction-past") != nil {
		t.Fatal("a confirmed ovulation must not raise the stale-projection notice")
	}
}
