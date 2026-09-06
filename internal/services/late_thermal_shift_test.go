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
// before any surface is asked about it, on both sides of the projected start:
// a shift confirmed on the projected day itself and one confirmed after it.
func TestConfirmedOvulationSurvivesTheProjectedNextPeriodStart(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		shiftDays int
		want      string
	}{
		{name: "confirmed on the projected start itself", shiftDays: 18, want: "2026-03-29"},
		{name: "confirmed after the projected start", shiftDays: 19, want: "2026-03-30"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			user, logs, stats, today := thermalShiftFixture(t, testCase.shiftDays)

			confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC)
			if !ok {
				t.Fatal("resolver: a late shift recorded after the projected next period start must still confirm")
			}
			if got := CalendarDayKey(confirmed); got != testCase.want {
				t.Fatalf("resolver: confirmed ovulation = %s, want %s", got, testCase.want)
			}
		})
	}
}

// TestLateShiftNamesOneDayOnEverySurfaceInsideTheInstance compares the four
// surfaces an owner reads this shift from — the stats chart, the calendar grid,
// the dashboard line and the JSON overview — against the one day the resolver
// names, so a surface that drifts fails on its own comparison rather than on
// two literals happening to agree. Since the fix the chart shares the series
// bound with the resolver, so its leg pins the marker-index convention; the
// other three read the resolver.
func TestLateShiftNamesOneDayOnEverySurfaceInsideTheInstance(t *testing.T) {
	user, logs, stats, today := lateThermalShiftFixture(t)
	if stats.CurrentCycleDay != 32 {
		t.Fatalf("fixture anchor: cycle day = %d, want 32 — the cohort is a cycle running late but not yet overdue", stats.CurrentCycleDay)
	}

	confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC)
	if !ok {
		t.Fatal("fixture: the late shift must confirm before the surfaces are compared")
	}
	confirmedKey := CalendarDayKey(confirmed)
	if confirmedKey != "2026-03-29" {
		t.Fatalf("fixture anchor: confirmed ovulation = %s, want 2026-03-29", confirmedKey)
	}
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
	days := BuildCalendarDayStates(user, monthStart, logs, stats, today, time.UTC)
	solid, tentative := ovulationMarkerKeys(days)
	if len(solid) != 1 || solid[0] != confirmedKey {
		t.Errorf("calendar: solid ovulation marker(s) = %v, want exactly [%s]", solid, confirmedKey)
	}
	if len(tentative) != 0 {
		t.Errorf("calendar: tentative ovulation marker(s) = %v, want none — the projection on %s must not linger beside the confirmed day", tentative, CalendarDayKey(stats.OvulationDate))
	}
	// The projected next period is NOT recomputed from the confirmed shift (a
	// separate decision, not taken), so on this cohort the confirmed day and the
	// first projected bleeding day are the same date and that one cell carries
	// both flags. Pinned so the overlap is a named state rather than a
	// coincidence a later reader has to rediscover.
	if confirmedDay := findCalendarDayStateByDateString(t, days, confirmedKey); !confirmedDay.IsPredicted {
		t.Errorf("calendar: %s IsPredicted = false, want true — the projected next period still starts on the day the shift confirms", confirmedKey)
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
	user, logs, stats, fixtureToday := lateThermalShiftFixture(t)
	confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, fixtureToday, time.UTC)
	if !ok {
		t.Fatal("fixture: the late shift must confirm before the cycle runs overdue")
	}
	confirmedKey := CalendarDayKey(confirmed)

	// Five days on: 28 + 7 = 35 is the last day the reference allows, so cycle
	// day 37 is overdue.
	today := AddCalendarDays(fixtureToday, 5, time.UTC)
	stats = atToday(stats, today)
	if stats.CurrentCycleDay != 37 {
		t.Fatalf("fixture anchor: cycle day = %d, want 37 — past the 28 + 7 the reference allows", stats.CurrentCycleDay)
	}

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
	days := BuildCalendarDayStates(user, monthStart, logs, stats, today, time.UTC)
	day := findCalendarDayStateByDateString(t, days, confirmedKey)
	if day.IsOvulation || day.IsTentativeOvulation {
		t.Errorf("calendar: %s IsOvulation=%t IsTentativeOvulation=%t, want both false while the cycle is overdue", confirmedKey, day.IsOvulation, day.IsTentativeOvulation)
	}

	chart := buildCurrentCycleBBTChart("en", stats, logs, today, time.UTC)
	if !chart.HasMarker {
		t.Error("chart: expected the marker to remain while overdue — it names a recorded shift, not a projection")
	}
}

// TestLateShiftSupersedesTheCurrentCyclesProjectionOnly pins the predicate's
// edge on the same fixture: a projection dated before NextPeriodStart is the
// current cycle's and is superseded by the shift; one on or after it is the
// next cycle's, which the shift says nothing about. Whether an egress caller
// still holds the former once today has passed the projected start is its
// anchor's business (ProjectCycleStart), not this test's.
func TestLateShiftSupersedesTheCurrentCyclesProjectionOnly(t *testing.T) {
	user, logs, stats, today := lateThermalShiftFixture(t)

	lastCurrentCycleDay := AddCalendarDays(stats.NextPeriodStart, -1, time.UTC)
	if !ConfirmedOvulationSupersedes(user, logs, stats, lastCurrentCycleDay, today, time.UTC) {
		t.Error("supersedes: a projection on the day before NextPeriodStart is the CURRENT cycle's and must be superseded")
	}

	if ConfirmedOvulationSupersedes(user, logs, stats, stats.NextPeriodStart, today, time.UTC) {
		t.Error("supersedes: a projection on NextPeriodStart itself belongs to the NEXT cycle and must not be superseded")
	}
}

// TestLateShiftDoesNotCountAReadingLoggedAfterToday pins the detection bound's
// other edge: the series runs through today and no further. At cycle day 31
// only two of the three elevated readings exist as of today — the third is
// dated tomorrow — so the streak is not yet three days long and nothing may be
// confirmed. This case catches only a bound that is too WIDE; a bound too
// narrow is TestConfirmedOvulationSurvivesTheProjectedNextPeriodStart's
// subject, where the third elevated day IS today.
func TestLateShiftDoesNotCountAReadingLoggedAfterToday(t *testing.T) {
	user, logs, stats, fixtureToday := lateThermalShiftFixture(t)

	// The positive control runs first: on the fixture's own today the third
	// elevated reading is recorded and the shift confirms, so the refusal below
	// is the bound's doing rather than a resolver that had stopped confirming
	// anything at all.
	if _, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, fixtureToday, time.UTC); !ok {
		t.Fatal("control: on the day the third elevated reading is recorded the shift must confirm")
	}

	today := AddCalendarDays(fixtureToday, -1, time.UTC)
	stats = atToday(stats, today)

	if _, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC); ok {
		t.Fatal("resolver: a reading dated after today must not help confirm a shift")
	}
}
