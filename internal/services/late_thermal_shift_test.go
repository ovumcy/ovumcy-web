package services

// late_thermal_shift_test.go — a thermal shift that lands after the model's
// projected next-period start is still an event of the CURRENT cycle, and every
// surface reading the shared resolver must confirm it. The two surfaces that
// leave the instance (the .ics feed and the webhook reminder) are not among
// them: they announce projections, and which projection they hold once today
// has passed the projected start is their anchor's business.

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// lateThermalShiftFixture slides confirmedOvulationFixture's series 18 days
// later: the model still projects the next period on 2026-03-29, but the
// coverline window now sits on cycle days 24-29 and the elevated streak on days
// 30-32, so the detector confirms 2026-03-29 — the projected start itself — on
// 2026-04-01, cycle day 32.
func lateThermalShiftFixture(t *testing.T) (*models.User, []models.DailyLog, CycleStats, time.Time) {
	t.Helper()
	return thermalShiftFixture(t, 18)
}

// TestConfirmedOvulationSurvivesTheProjectedNextPeriodStart pins the resolver
// before any surface is asked about it.
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

// TestLateShiftNamesOneDayOnEverySurfaceInsideTheInstance compares the four
// surfaces an owner reads this shift from — the stats chart, the calendar grid,
// the dashboard line and the JSON overview — against the one day the resolver
// names, so a surface that drifts fails on its own comparison rather than on
// two literals happening to agree. The chart already named the day before the
// fix (its window was never bounded by the projection); the other three read
// the resolver.
func TestLateShiftNamesOneDayOnEverySurfaceInsideTheInstance(t *testing.T) {
	user, logs, stats, today := lateThermalShiftFixture(t)

	confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC)
	if !ok {
		t.Fatal("fixture: the late shift must confirm before the surfaces are compared")
	}
	confirmedKey := CalendarDayKey(confirmed)
	cycleStart := CalendarDay(stats.LastPeriodStart, time.UTC)

	// (i) The stats chart marker, read back as the day its index names.
	chart := buildCurrentCycleBBTChart("en", stats, logs, today, time.UTC)
	if !chart.HasMarker {
		t.Error("chart: expected a probable-ovulation marker for the late shift")
	} else if got := CalendarDayKey(AddCalendarDays(cycleStart, chart.MarkerIndex, time.UTC)); got != confirmedKey {
		t.Errorf("chart: marker on %s, want %s", got, confirmedKey)
	}

	// (ii) The calendar grid's solid marker.
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	solid := make([]string, 0, 1)
	tentative := make([]string, 0, 1)
	for _, day := range BuildCalendarDayStates(user, monthStart, logs, stats, today, time.UTC) {
		if day.IsOvulation {
			solid = append(solid, day.DateString)
		}
		if day.IsTentativeOvulation {
			tentative = append(tentative, day.DateString)
		}
	}
	if len(solid) != 1 || solid[0] != confirmedKey {
		t.Errorf("calendar: solid ovulation marker(s) = %v, want exactly [%s]", solid, confirmedKey)
	}
	if len(tentative) != 0 {
		t.Errorf("calendar: tentative ovulation marker(s) = %v, want none — the projection on %s must not linger beside the confirmed day", tentative, CalendarDayKey(stats.OvulationDate))
	}

	// (iii) The dashboard's ovulation line.
	context := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if got := CalendarDayKey(context.DisplayOvulationDate); got != confirmedKey {
		t.Errorf("dashboard: ovulation line = %s, want %s", got, confirmedKey)
	}
	if !context.DisplayOvulationConfirmed {
		t.Error("dashboard: DisplayOvulationConfirmed = false, want true for a detector-named day")
	}

	// (iv) The JSON API's overview stats.
	published, _, confirmedOvulation := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if got := CalendarDayKey(published.OvulationDate); got != confirmedKey {
		t.Errorf("API: published OvulationDate = %s, want %s", got, confirmedKey)
	}
	if !confirmedOvulation {
		t.Error("API: confirmedOvulation = false, want true for a detector-named day")
	}
}

// TestLateShiftStaysWithheldWhenTheCycleIsOverdue is the suppression control:
// once the cycle is overdue (DashboardCycleOverdue, > reference + 7) the
// resolver and every surface reading it withhold the confirmed day like any
// other projection-adjacent signal. The chart is not one of those surfaces —
// its marker reads recorded temperatures, never a projection — so it keeps
// naming the same shift; that is the one place the four may differ.
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

	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	day := findCalendarDayStateByDateString(t, BuildCalendarDayStates(user, monthStart, logs, stats, today, time.UTC), "2026-03-29")
	if day.IsOvulation || day.IsTentativeOvulation {
		t.Errorf("calendar: 2026-03-29 IsOvulation=%t IsTentativeOvulation=%t, want both false while the cycle is overdue", day.IsOvulation, day.IsTentativeOvulation)
	}

	chart := buildCurrentCycleBBTChart("en", stats, logs, today, time.UTC)
	if !chart.HasMarker {
		t.Error("chart: expected the marker to remain while overdue — it names a recorded shift, not a projection")
	}
}

// TestLateShiftSupersedesTheCurrentCyclesProjectionOnly pins the predicate's
// contract on the same fixture: a projection dated before NextPeriodStart is
// the current cycle's and is superseded by the shift; one on or after it is the
// next cycle's, which the shift says nothing about. Whether an egress caller
// still holds the former once today has passed the projected start is its
// anchor's business (ProjectCycleStart), not this test's.
func TestLateShiftSupersedesTheCurrentCyclesProjectionOnly(t *testing.T) {
	user, logs, stats, today := lateThermalShiftFixture(t)

	if !ConfirmedOvulationSupersedes(user, logs, stats, stats.OvulationDate, today, time.UTC) {
		t.Error("supersedes: the CURRENT cycle's projection, which the shift answered, must be superseded")
	}

	nextCycleProjection := AddCalendarDays(stats.NextPeriodStart, 14, time.UTC)
	if ConfirmedOvulationSupersedes(user, logs, stats, nextCycleProjection, today, time.UTC) {
		t.Error("supersedes: a NEXT-cycle projection — which this shift says nothing about — must not be superseded")
	}
}

// TestLateShiftDoesNotCountAReadingLoggedAfterToday pins the detection bound's
// other edge: the series runs through today and no further. At cycle day 31
// only two of the three elevated readings exist as of today — the third is
// dated tomorrow — so the streak is not yet three days long and nothing may be
// confirmed.
func TestLateShiftDoesNotCountAReadingLoggedAfterToday(t *testing.T) {
	user, logs, stats, _ := lateThermalShiftFixture(t)
	today := time.Date(2026, time.March, 31, 0, 0, 0, 0, time.UTC)
	stats.CurrentCycleDay = 31

	if _, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC); ok {
		t.Fatal("resolver: a reading dated after today must not help confirm a shift")
	}
}
