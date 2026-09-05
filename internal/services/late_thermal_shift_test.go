package services

// late_thermal_shift_test.go — a thermal shift that lands AFTER the model's
// projected next-period start is still an event of the CURRENT cycle, and the
// shared resolver must confirm it on every surface that reads it: the stats
// chart already did (its own detection window runs to today+1), but the
// calendar, the dashboard and the JSON API all read ConfirmedCurrentCycleOvulation,
// which bounded its detection series at the PROJECTED stats.NextPeriodStart
// instead — so a late shift was visible on the chart and invisible everywhere
// else, one cycle for the length of an owner's whole thermal record past the
// projection.

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// lateThermalShiftFixture builds a cycle that runs past its own projection: a
// 28-day model (median/average 28, luteal 14) projects the next period on
// 2026-03-29, but the owner's temperatures do not rise until cycle day 30 —
// two days into what the model already calls the next cycle. The 3-over-6
// detector's 6-day coverline window sits on cycle days 24-29 (36.20) and the
// elevated streak on cycle days 30-32 (36.50), so the shared detector confirms
// ovulation on cycle day 29, 2026-03-29 — the day before the first elevated
// reading, which is itself after the projected start.
func lateThermalShiftFixture(t *testing.T) (*models.User, []models.DailyLog, CycleStats, time.Time) {
	t.Helper()

	cycleStart := cyclesignalsCovDay(t, "2026-03-01")
	logs := []models.DailyLog{
		{Date: cycleStart, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: cyclesignalsCovDay(t, "2026-03-02"), IsPeriod: true, Flow: models.FlowMedium},
	}
	for _, day := range []string{"2026-03-24", "2026-03-25", "2026-03-26", "2026-03-27", "2026-03-28", "2026-03-29"} {
		logs = append(logs, cyclesignalsCovBBTLog(t, day, 36.20))
	}
	for _, day := range []string{"2026-03-30", "2026-03-31", "2026-04-01"} {
		logs = append(logs, cyclesignalsCovBBTLog(t, day, 36.50))
	}

	user := &models.User{ID: 1, Role: models.RoleOwner, TrackBBT: true}
	stats := CycleStats{
		CompletedCycleCount: 3,
		MedianCycleLength:   28,
		AverageCycleLength:  28,
		AveragePeriodLength: 5,
		LutealPhase:         14,
		CurrentCycleDay:     32,
		LastPeriodStart:     cycleStart,
		OvulationDate:       cyclesignalsCovDay(t, "2026-03-14"),
		NextPeriodStart:     cyclesignalsCovDay(t, "2026-03-29"),
	}
	today := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	return user, logs, stats, today
}

// TestConfirmedOvulationSurvivesTheProjectedNextPeriodStart pins the resolver
// before any surface is asked about it: a shift confirmed after
// stats.NextPeriodStart must still be returned, not cut off at the model's own
// prediction.
func TestConfirmedOvulationSurvivesTheProjectedNextPeriodStart(t *testing.T) {
	user, logs, stats, today := lateThermalShiftFixture(t)

	confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC)
	if !ok {
		t.Fatal("resolver: a late shift recorded after the projected next period start must still confirm")
	}
	if got := CalendarDayKey(confirmed); got != "2026-03-29" {
		t.Fatalf("resolver: confirmed ovulation = %s, want 2026-03-29", got)
	}
}

// TestLateShiftNamesOneDayOnEverySurface checks the four surfaces an owner can
// read this shift from against the ONE fixture above. The stats chart already
// names 2026-03-29 before the fix (its own detection window is not bounded by
// stats.NextPeriodStart); the other three read the shared resolver and must
// name the same day once it is fixed to agree with the chart.
func TestLateShiftNamesOneDayOnEverySurface(t *testing.T) {
	user, logs, stats, today := lateThermalShiftFixture(t)

	// (i) The stats chart marker.
	chart := buildCurrentCycleBBTChart("en", stats, logs, today, time.UTC)
	if !chart.HasMarker {
		t.Error("chart: expected a probable-ovulation marker for the late shift")
	} else if chart.MarkerIndex != 28 {
		t.Errorf("chart: marker index = %d, want 28 (0-based 2026-03-29)", chart.MarkerIndex)
	}

	// (ii) The calendar grid's solid marker.
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	days := BuildCalendarDayStates(user, monthStart, logs, stats, today, time.UTC)
	solid := make([]string, 0, 1)
	tentative := make([]string, 0, 1)
	for _, day := range days {
		if day.IsOvulation {
			solid = append(solid, day.DateString)
		}
		if day.IsTentativeOvulation {
			tentative = append(tentative, day.DateString)
		}
	}
	if len(solid) != 1 || solid[0] != "2026-03-29" {
		t.Errorf("calendar: solid ovulation marker(s) = %v, want exactly [2026-03-29]", solid)
	}
	if len(tentative) != 0 {
		t.Errorf("calendar: tentative ovulation marker(s) = %v, want none — the projection on 2026-03-14 must not linger beside the confirmed day", tentative)
	}

	// (iii) The dashboard's ovulation line.
	context := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if got := CalendarDayKey(context.DisplayOvulationDate); got != "2026-03-29" {
		t.Errorf("dashboard: ovulation line = %s, want 2026-03-29", got)
	}
	if !context.DisplayOvulationConfirmed {
		t.Error("dashboard: DisplayOvulationConfirmed = false, want true for a detector-named day")
	}

	// (iv) The JSON API's overview stats.
	published, _, confirmedOvulation := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if got := CalendarDayKey(published.OvulationDate); got != "2026-03-29" {
		t.Errorf("API: published OvulationDate = %s, want 2026-03-29", got)
	}
	if !confirmedOvulation {
		t.Error("API: confirmedOvulation = false, want true for a detector-named day")
	}
}

// TestLateShiftStaysWithheldWhenTheCycleIsOverdue is the suppression control:
// once the cycle is overdue (DashboardCycleOverdue, > reference + 7), the
// resolver and the surfaces reading it must withhold the confirmed day like
// any other projection-adjacent signal. The chart is not one of those
// surfaces — its marker reads recorded temperatures, never a projection — so
// it keeps showing the same shift.
func TestLateShiftStaysWithheldWhenTheCycleIsOverdue(t *testing.T) {
	user, logs, stats, _ := lateThermalShiftFixture(t)
	today := time.Date(2026, time.April, 6, 0, 0, 0, 0, time.UTC)
	stats.CurrentCycleDay = 37 // 28 + 7 = 35: cycle day 37 is overdue.

	if _, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC); ok {
		t.Fatal("resolver: an overdue cycle must withhold the confirmed day like any other projection")
	}

	published, suppression, confirmedOvulation := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if !published.OvulationDate.IsZero() {
		t.Errorf("API: published OvulationDate = %s, want zero while the cycle is overdue", CalendarDayKey(published.OvulationDate))
	}
	if !suppression.FertilitySuppressed {
		t.Error("API: suppression.FertilitySuppressed = false, want true for an overdue cycle")
	}
	if confirmedOvulation {
		t.Error("API: confirmedOvulation = true, want false while the cycle is overdue")
	}

	context := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if !context.DisplayOvulationDate.IsZero() {
		t.Errorf("dashboard: ovulation line = %s, want none while the cycle is overdue", CalendarDayKey(context.DisplayOvulationDate))
	}

	// The chart is unaffected: it names a RECORDED shift, not a projection, so
	// overdue suppression — which exists to stop announcing dates the model can
	// no longer support — has nothing to withhold here.
	chart := buildCurrentCycleBBTChart("en", stats, logs, today, time.UTC)
	if !chart.HasMarker {
		t.Error("chart: expected the marker to remain while overdue — it names a recorded shift, not a projection")
	}
}

// TestLateShiftDoesNotCountAReadingLoggedAfterToday pins the other bound: the
// resolver reads logs up to the owner's today, never beyond it. At cycle day
// 31 only two of the three elevated readings have been recorded as of today —
// the third is dated tomorrow — so the streak is not yet three days long and
// nothing may be confirmed.
func TestLateShiftDoesNotCountAReadingLoggedAfterToday(t *testing.T) {
	user, logs, stats, _ := lateThermalShiftFixture(t)
	today := time.Date(2026, time.March, 31, 0, 0, 0, 0, time.UTC)
	stats.CurrentCycleDay = 31

	if _, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC); ok {
		t.Fatal("resolver: a reading dated after today must not help confirm a shift")
	}
}
