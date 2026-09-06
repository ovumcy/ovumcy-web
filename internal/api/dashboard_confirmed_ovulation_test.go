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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
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
// CURRENT cycle, so the model projects a next period the recorded shift then
// straddles. The recorded series never moves — the coverline window is
// today-8..today-3 and the elevated streak today-2..today — so the detector
// names today-3 in both cases; what moves is the model's own projection, slid
// back by daysPastProjection:
//
//	0 — the cycle opens at today-31 (cycle day 32), the next period is projected
//	    on today-3, and the confirmed day falls ON that projected start;
//	1 — the cycle opens at today-32 (cycle day 33, still <= 28+7 and so not
//	    overdue), the next period is projected on today-4, and the confirmed day
//	    falls a day AFTER it — the side a detector bounded at the projection
//	    cannot reach at all rather than merely truncating.
//
// Returns the day the detector confirms and the projected next-period start it
// is measured against.
func seedLateThermalShiftCycle(t *testing.T, database *gorm.DB, user models.User, today time.Time, daysPastProjection int) (time.Time, time.Time) {
	t.Helper()

	seedStatsOverviewCycleHistory(t, database, user, today,
		115+daysPastProjection, 87+daysPastProjection, 59+daysPastProjection, 31+daysPastProjection)
	firstLowDay := services.AddCalendarDays(today, -8, time.UTC)
	for offset := range 6 {
		seedStatsOverviewLog(t, database, models.DailyLog{
			UserID: user.ID,
			Date:   services.AddCalendarDays(firstLowDay, offset, time.UTC),
			BBT:    new(dashboardConfirmedOvulationLowBBT),
		})
	}
	for offset := range 3 {
		seedStatsOverviewLog(t, database, models.DailyLog{
			UserID: user.ID,
			Date:   services.AddCalendarDays(firstLowDay, 6+offset, time.UTC),
			BBT:    new(dashboardConfirmedOvulationHighBBT),
		})
	}
	confirmed := services.AddCalendarDays(today, -3, time.UTC)
	projected := services.AddCalendarDays(today, -3-daysPastProjection, time.UTC)
	return confirmed, projected
}

// dashboardOvulationSlotText reads the ovulation slot out of the dashboard's
// status header, failing on either anchor on the way: an absent slot must read
// as a fixture that drifted out of its cohort, never as a slot naming nothing.
// The parsed document comes back with the text so a caller can assert on a
// sibling hook without a second request.
func dashboardOvulationSlotText(t *testing.T, app *fiber.App, authCookie string) (string, *html.Node) {
	t.Helper()

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
	return normalizeHTMLText(htmlNodeText(ovulation)), document
}

// TestDashboardNamesALateShiftAfterTheProjectedNextPeriodStart is the rendered
// half of TestStatsOverviewConfirmsALateShiftAfterTheProjectedNextPeriodStart
// (stats_overview_contract_test.go): the same cohort read through the
// dashboard's ovulation slot rather than the JSON payload, so the two
// owner-facing surfaces cannot name different days for one late shift. Both
// sides of the projected start are exercised — a shift confirmed ON it and one
// confirmed a day AFTER it — because a bound that stops AT the projection and
// one that merely truncates the series behave alike on the first.
func TestDashboardNamesALateShiftAfterTheProjectedNextPeriodStart(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		daysPastProjection int
	}{
		{name: "on the projected start", daysPastProjection: 0},
		{name: "after the projected start", daysPastProjection: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			email := fmt.Sprintf("dashboard-late-shift-%d@example.com", testCase.daysPastProjection)
			user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
			// The rendered ovulation slot exists only for an account tracking to
			// conceive (resolveDashboardTimingFrame, dashboard_view_service.go).
			updateStatsOverviewUser(t, database, user, map[string]any{"track_bbt": true, "usage_goal": models.UsageGoalTrying})
			confirmedDay, projectedStart := seedLateThermalShiftCycle(t, database, user, today, testCase.daysPastProjection)

			slot, _ := dashboardOvulationSlotText(t, app, authCookie)
			if want := services.LocalizedDateDisplay("en", confirmedDay); !strings.Contains(slot, want) {
				t.Fatalf("the ovulation slot = %q, want the BBT-confirmed %q — a shift recorded after the projected next period start is still this cycle's", slot, want)
			}
			if testCase.daysPastProjection == 0 {
				// On this side the two days ARE the same date, so there is no
				// second date to be absent.
				return
			}
			if services.CalendarDaysBetween(projectedStart, confirmedDay) != testCase.daysPastProjection {
				t.Fatalf("fixture anchor: confirmed day %s is not %d day(s) past the projected start %s", confirmedDay.Format("2006-01-02"), testCase.daysPastProjection, projectedStart.Format("2006-01-02"))
			}
			if projected := services.LocalizedDateDisplay("en", projectedStart); strings.Contains(slot, projected) {
				t.Fatalf("the ovulation slot names the projected next period start %q beside the confirmed day: %q", projected, slot)
			}
		})
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

	updateStatsOverviewUser(t, database, user, map[string]any{
		"cycle_length":      cycleLength,
		"period_length":     periodLength,
		"last_period_start": cycleStart,
		"usage_goal":        models.UsageGoalTrying,
		"irregular_cycle":   true,
		"track_bbt":         true,
	})

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
	slot, document := dashboardOvulationSlotText(t, app, authCookie)
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
