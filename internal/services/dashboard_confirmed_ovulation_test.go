package services

// dashboard_confirmed_ovulation_test.go — the dashboard's ovulation line names
// the day the temperatures point at, not the projection that day superseded.
//
// The calendar's solid marker and the stats chart both moved onto the
// detector's own date; the dashboard line did not. It is fed by
// DashboardUpcomingPredictions, which rolls its anchor to the NEXT cycle only
// when CalendarDaysBetween(window.OvulationDate, today) > 0 — so on the very day
// the projection fell on, the difference is zero, the anchor stays, and the line
// announces an ovulation the recorded temperatures place several days earlier.
// One shift, two dates, on the two surfaces an owner reads side by side.
//
// Both surfaces now resolve the day through ConfirmedCurrentCycleOvulation. The
// substitution changes only WHICH day is named: whether a day may be named at
// all stays with the suppression gates (prediction-display.md).

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// confirmedOvulationFixture builds the reproducing cycle: it starts 2026-03-01,
// runs 28 days with a 14-day luteal phase, so the model projects ovulation on
// cycle day 14 — 2026-03-14, which is also "today" in the test. Undisturbed
// temperatures on cycle days 6..11 fill the coverline window and days 12..14 are
// elevated, so the shared 3-over-6 detector names cycle day 11, 2026-03-11.
func confirmedOvulationFixture(t *testing.T) (*models.User, []models.DailyLog, CycleStats, time.Time) {
	t.Helper()

	cycleStart := cyclesignalsCovDay(t, "2026-03-01")
	logs := []models.DailyLog{
		{Date: cycleStart, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: cyclesignalsCovDay(t, "2026-03-02"), IsPeriod: true, Flow: models.FlowMedium},
	}
	for _, day := range []string{"2026-03-06", "2026-03-07", "2026-03-08", "2026-03-09", "2026-03-10", "2026-03-11"} {
		logs = append(logs, cyclesignalsCovBBTLog(t, day, 36.20))
	}
	for _, day := range []string{"2026-03-12", "2026-03-13", "2026-03-14"} {
		logs = append(logs, cyclesignalsCovBBTLog(t, day, 36.50))
	}

	user := &models.User{ID: 1, Role: models.RoleOwner, TrackBBT: true}
	stats := CycleStats{
		CompletedCycleCount: 3,
		MedianCycleLength:   28,
		AverageCycleLength:  28,
		AveragePeriodLength: 5,
		LutealPhase:         14,
		CurrentCycleDay:     14,
		LastPeriodStart:     cycleStart,
		OvulationDate:       cyclesignalsCovDay(t, "2026-03-14"),
		NextPeriodStart:     cyclesignalsCovDay(t, "2026-03-29"),
	}
	today := time.Date(2026, time.March, 14, 0, 0, 0, 0, time.UTC)
	return user, logs, stats, today
}

// TestConfirmedCurrentCycleOvulationReadsTheDetectorsDay pins the shared
// resolver before either surface is asked about it, so a failure below reads as
// a surface defect rather than as a fixture that never confirmed anything.
func TestConfirmedCurrentCycleOvulationReadsTheDetectorsDay(t *testing.T) {
	user, logs, stats, today := confirmedOvulationFixture(t)

	confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC)
	if !ok {
		t.Fatal("fixture: the 3-over-6 detector must confirm a shift in this cycle")
	}
	if got := CalendarDayKey(confirmed); got != "2026-03-11" {
		t.Fatalf("fixture: confirmed ovulation = %s, want 2026-03-11", got)
	}
	if got := CalendarDayKey(stats.OvulationDate); got != "2026-03-14" {
		t.Fatalf("fixture: the model must still project 2026-03-14, got %s", got)
	}
}

func TestDashboardOvulationLineNamesTheConfirmedDay(t *testing.T) {
	user, logs, stats, today := confirmedOvulationFixture(t)

	context := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)

	if got := CalendarDayKey(context.DisplayOvulationDate); got != "2026-03-11" {
		t.Fatalf("dashboard ovulation line = %s, want 2026-03-11: the temperatures confirm that day and the calendar marker already names it", got)
	}
	if !context.OvulationInPast {
		t.Fatal("a confirmed ovulation three days behind the owner must render as past, not as still upcoming")
	}
}

// TestDashboardOvulationLineKeepsTheProjectionWithoutAShift is the control: the
// same cycle with no thermal evidence must still name the projected day and
// must not be reported as past. Without it the assertion above would also pass
// for a line that had simply stopped showing a projection.
func TestDashboardOvulationLineKeepsTheProjectionWithoutAShift(t *testing.T) {
	user, _, stats, today := confirmedOvulationFixture(t)

	periodOnly := []models.DailyLog{
		{Date: cyclesignalsCovDay(t, "2026-03-01"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: cyclesignalsCovDay(t, "2026-03-02"), IsPeriod: true, Flow: models.FlowMedium},
	}

	context := BuildDashboardCycleContext(user, periodOnly, stats, today, time.UTC)

	if got := CalendarDayKey(context.DisplayOvulationDate); got != "2026-03-14" {
		t.Fatalf("dashboard ovulation line = %s, want the projected 2026-03-14 when no shift is recorded", got)
	}
	if context.OvulationInPast {
		t.Fatal("the projected day is today, so it must not be reported as past")
	}
}

// TestDashboardAndCalendarNameOneDayForOneShift is the point of the shared
// resolver: the two surfaces an owner reads side by side must not disagree.
func TestDashboardAndCalendarNameOneDayForOneShift(t *testing.T) {
	user, logs, stats, today := confirmedOvulationFixture(t)
	user.WeekStartsOn = models.WeekStartMonday

	context := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	days := BuildCalendarDayStates(user, monthStart, logs, stats, today, time.UTC)

	solid := make([]string, 0, 1)
	for _, day := range days {
		if day.IsOvulation {
			solid = append(solid, day.DateString)
		}
	}

	if len(solid) != 1 {
		t.Fatalf("expected exactly one solid ovulation marker in the current cycle's month, got %v", solid)
	}
	if solid[0] != CalendarDayKey(context.DisplayOvulationDate) {
		t.Fatalf("calendar marks %s while the dashboard line says %s — one shift must produce one day", solid[0], CalendarDayKey(context.DisplayOvulationDate))
	}
}

// TestDashboardOvulationLineLeavesSuppressionAlone pins the boundary the
// substitution must not cross: a confirmed observation changes WHICH day is
// named, never whether one is named. An unpredictable-cycle account withholds
// the ovulation estimate, and a recorded thermal shift must not reinstate it.
func TestDashboardOvulationLineLeavesSuppressionAlone(t *testing.T) {
	user, logs, stats, today := confirmedOvulationFixture(t)
	user.UnpredictableCycle = true

	context := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)

	if !context.DisplayOvulationDate.IsZero() {
		t.Fatalf("suppressed account still renders an ovulation date (%s): a confirmed shift must not be a way around the gate", CalendarDayKey(context.DisplayOvulationDate))
	}
}

// TestDashboardOvulationLineOutranksTheIrregularRange and its sibling below
// cover the two ways the dashboard expresses PROJECTION uncertainty. Neither is
// a suppression gate, and neither is about a day the temperatures have already
// named — but both used to discard the confirmed date, leaving the calendar
// marking a day the dashboard would not name. The calendar gates this signal on
// FertilityProjectionSuppressed alone, which neither cohort here meets.
func TestDashboardOvulationLineOutranksTheIrregularRange(t *testing.T) {
	user, logs, stats, today := confirmedOvulationFixture(t)
	// dashboardIrregularPredictionRangeEnabled: irregular, >= 3 completed
	// cycles, and a real min/max spread.
	user.IrregularCycle = true
	stats.MinCycleLength = 25
	stats.MaxCycleLength = 33

	context := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)

	if context.DisplayOvulationUseRange {
		t.Error("a confirmed ovulation must be named as a day, not widened into a range built from cycle-length spread")
	}
	if got := CalendarDayKey(context.DisplayOvulationDate); got != "2026-03-11" {
		t.Fatalf("dashboard ovulation line = %s, want 2026-03-11 for an irregular account whose temperatures confirmed the day", got)
	}
}

func TestDashboardOvulationLineOutranksTheThinHistoryWithholding(t *testing.T) {
	user, logs, stats, today := confirmedOvulationFixture(t)
	// dashboardNeedsOvulationData: irregular with fewer than three completed
	// cycles. One completed cycle keeps the first-cycle floor from firing, so
	// the calendar still marks the detector's day.
	user.IrregularCycle = true
	stats.CompletedCycleCount = 1

	context := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)

	if got := CalendarDayKey(context.DisplayOvulationDate); got != "2026-03-11" {
		t.Fatalf("dashboard ovulation line = %s, want 2026-03-11: \"needs more cycles\" is about a projection, not about a recorded thermal shift", got)
	}
}

// TestConfirmedOvulationStopsAtTheFirstCycleFloor is the other side: the
// calendar withholds the BBT pass entirely under FertilityProjectionSuppressed,
// so the resolver must too, or the dashboard would name a day the grid refuses
// to mark — the same divergence pointing the other way.
func TestConfirmedOvulationStopsAtTheFirstCycleFloor(t *testing.T) {
	user, logs, stats, today := confirmedOvulationFixture(t)
	stats.CompletedCycleCount = 0

	if _, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC); ok {
		t.Fatal("with no completed cycle the fertility projection is suppressed on every surface, so nothing may be confirmed here either")
	}

	context := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if got := CalendarDayKey(context.DisplayOvulationDate); got == "2026-03-11" {
		t.Fatal("the dashboard must not name the detector's day while the calendar withholds it under the first-cycle floor")
	}
}
