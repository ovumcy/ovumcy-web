package api

// dashboard_confirmed_ovulation_qualifier_test.go — the projection's own
// qualifier does not follow a measured day onto the status line.
//
// The "approximately" marker rides DisplayOvulationExact, which reports whether
// CalcOvulationDay had to CLAMP the luteal phase into a short cycle. That is a
// property of the projection's arithmetic; a day the temperatures named did not
// come out of it. Carrying the marker across the substitution captioned a
// recorded reading as an approximation of itself.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestDashboardDropsTheProjectionQualifierFromAConfirmedDay needs the narrow
// band where the header shows an INEXACT estimate at all, so it reuses the
// inexact-ovulation fixture's shape: a 16-day reference cycle with the default
// 14-day luteal phase forces the clamp (CalcOvulationDay caps the luteal phase
// at cycleLength-5), a 3-day period keeps the cycle hero drawable, and one
// previous cycle clears the first-cycle fertility floor. On top of that it
// records a thermal shift, which is what the substitution needs.
//
// The hero's own qualifier is the anchor: it rides the same DisplayOvulationExact
// and must still render, so a missing ovulation marker below is the substitution
// dropping it rather than the fixture having drifted into an exact projection.
func TestDashboardDropsTheProjectionQualifierFromAConfirmedDay(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-confirmed-qualifier@example.com", "StrongPass1", true)

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	const cycleLength = 16
	const periodLength = 3
	cycleStart := today.AddDate(0, 0, -13)
	confirmedDay := cycleStart.AddDate(0, 0, 10)

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      cycleLength,
		"period_length":     periodLength,
		"last_period_start": cycleStart,
		"usage_goal":        models.UsageGoalTrying,
		"track_bbt":         true,
	}).Error; err != nil {
		t.Fatalf("update confirmed-qualifier cycle context: %v", err)
	}
	for _, start := range []time.Time{cycleStart.AddDate(0, 0, -cycleLength), cycleStart} {
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
	if dashboardElementByDataAttr(header, "data-dashboard-estimate-qualifier") == nil {
		t.Fatal("fixture anchor: the projection must still be inexact, or this test proves nothing")
	}

	slot := normalizeHTMLText(htmlNodeText(ovulation))
	if want := services.LocalizedDateDisplay("en", confirmedDay); !strings.Contains(slot, want) {
		t.Fatalf("fixture anchor: the slot must name the detector's day %q, got %q", want, slot)
	}
	if dashboardElementByDataAttr(ovulation, "data-dashboard-ovulation-approximate") != nil {
		t.Fatalf("a measured day carried the projection's approximate marker: %q", slot)
	}
}
