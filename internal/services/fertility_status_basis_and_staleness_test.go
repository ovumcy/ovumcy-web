package services

// fertility_status_basis_and_staleness_test.go — what the published fertility
// status may claim, and against which window.
//
// Three defects shared one root: a status read off cycle arithmetic was
// published as if it were a fact about the owner's body. Outside a six-day
// median window the API said "not fertile"; in the irregular mode the
// dashboard shows a multi-day ovulation range for, the status still read the
// six median days; and once the running cycle had outrun its reference length
// — where both owner pages print "unknown" — the API kept publishing the
// phase and the status computed against a window the cycle had passed.

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// cycleStartLogs records one explicit cycle start per date.
func cycleStartLogs(t *testing.T, days ...string) []models.DailyLog {
	t.Helper()

	logs := make([]models.DailyLog, 0, len(days))
	for _, day := range days {
		logs = append(logs, models.DailyLog{Date: mustParseDay(t, day), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium})
	}
	return logs
}

// irregularSpreadFixture is an owner in irregular-cycle mode with three
// completed cycles of 24, 30 and 36 days, the running cycle starting
// 2026-05-01. With the 14-day default luteal phase:
//   - the median (30) window is 2026-05-11..16, ovulation 2026-05-16;
//   - the shortest cycle (24) ovulates 2026-05-10, so its window opens 05-05;
//   - the longest cycle (36) ovulates 2026-05-22.
func irregularSpreadFixture(t *testing.T) (*models.User, []models.DailyLog) {
	t.Helper()

	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, IrregularCycle: true}
	return user, cycleStartLogs(t, "2026-01-31", "2026-02-24", "2026-03-26", "2026-05-01")
}

func TestIrregularModeReadsTheStatusAcrossTheWholeOvulationRange(t *testing.T) {
	user, logs := irregularSpreadFixture(t)
	periodStart := mustParseDay(t, "2026-05-01")

	shortest := PredictCycleWindow(periodStart, 24, 14)
	longest := PredictCycleWindow(periodStart, 36, 14)
	median := PredictCycleWindow(periodStart, 30, 14)
	if !shortest.Calculable || !longest.Calculable || !median.Calculable {
		t.Fatal("fixture: every length in the spread must place an ovulation")
	}
	wantStart := shortest.OvulationDate.AddDate(0, 0, -5)
	wantEnd := longest.OvulationDate

	for _, tc := range []struct {
		name  string
		today string
		want  string
	}{
		{"the day before the shortest cycle's window", "2026-05-04", FertilityStatusOutsideEstimatedWindow},
		{"the shortest cycle's window start", "2026-05-05", FertilityStatusFertile},
		{"inside the range, before the median window", "2026-05-08", FertilityStatusFertile},
		{"inside the range, after the median ovulation", "2026-05-19", FertilityStatusFertile},
		{"the longest cycle's ovulation", "2026-05-22", FertilityStatusFertile},
		{"the day after the longest cycle's ovulation", "2026-05-23", FertilityStatusOutsideEstimatedWindow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := mustParseDay(t, tc.today)
			stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)

			if stats.MinCycleLength != 24 || stats.MaxCycleLength != 36 || stats.MedianCycleLength != 30 {
				t.Fatalf("fixture: lengths min/median/max = %d/%d/%d, want 24/30/36", stats.MinCycleLength, stats.MedianCycleLength, stats.MaxCycleLength)
			}
			if !sameDay(stats.FertilityWindowStart, wantStart) || !sameDay(stats.FertilityWindowEnd, wantEnd) {
				t.Fatalf("window = %s..%s, want %s..%s (ovulation(min) - 5 .. ovulation(max))",
					CalendarDayKey(stats.FertilityWindowStart), CalendarDayKey(stats.FertilityWindowEnd), CalendarDayKey(wantStart), CalendarDayKey(wantEnd))
			}
			if !sameDay(stats.OvulationDate, median.OvulationDate) {
				t.Fatalf("ovulation date = %s, want the median projection's %s — the range widens the window, not the day", CalendarDayKey(stats.OvulationDate), CalendarDayKey(median.OvulationDate))
			}
			if stats.CurrentFertility != tc.want || stats.FertilityBasis != FertilityBasisProjection {
				t.Fatalf("status %q basis %q, want %q on the projection", stats.CurrentFertility, stats.FertilityBasis, tc.want)
			}

			// The JSON API reads the same window through the published copy.
			published, _, _ := PublishedOverviewStats(user, logs, stats, now, time.UTC)
			if published.CurrentFertility != tc.want || published.FertilityBasis != FertilityBasisProjection {
				t.Fatalf("published status %q basis %q, want %q on the projection", published.CurrentFertility, published.FertilityBasis, tc.want)
			}
		})
	}

	// The calendar grid shades the same days the status calls fertile.
	now := mustParseDay(t, "2026-05-08")
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	shaded := make(map[string]bool)
	for _, day := range BuildCalendarDayStates(user, mustParseDay(t, "2026-05-01"), logs, stats, now, time.UTC) {
		if day.IsFertilityEdge || day.IsFertilityPeak {
			shaded[day.DateString] = true
		}
	}
	for _, day := range []string{"2026-05-05", "2026-05-08", "2026-05-19", "2026-05-22"} {
		if !shaded[day] {
			t.Fatalf("the calendar does not shade %s, which the status reads as fertile", day)
		}
	}
	for _, day := range []string{"2026-05-04", "2026-05-23"} {
		if shaded[day] {
			t.Fatalf("the calendar shades %s, outside the window the status reads", day)
		}
	}
}

// TestRegularModeKeepsTheMedianWindow is the positive control for the widening:
// the same history without the irregular setting keeps the six median days.
func TestRegularModeKeepsTheMedianWindow(t *testing.T) {
	user, logs := irregularSpreadFixture(t)
	user.IrregularCycle = false

	now := mustParseDay(t, "2026-05-08")
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if got, want := CalendarDayKey(stats.FertilityWindowStart)+".."+CalendarDayKey(stats.FertilityWindowEnd), "2026-05-11..2026-05-16"; got != want {
		t.Fatalf("window = %s, want the median %s", got, want)
	}
	if stats.CurrentFertility != FertilityStatusOutsideEstimatedWindow || stats.FertilityBasis != FertilityBasisProjection {
		t.Fatalf("status %q basis %q, want outside_estimated_window on the projection", stats.CurrentFertility, stats.FertilityBasis)
	}
}

// TestIrregularWindowOpensAtTheCycleStartWhenTheShortestCycleHoldsNoOvulation:
// a recorded cycle too short for the arithmetic to place an ovulation in
// (CalcOvulationDay refuses anything under 15 days) starts the window at the
// clamp PredictCycleWindow applies to every window start, the cycle start.
func TestIrregularWindowOpensAtTheCycleStartWhenTheShortestCycleHoldsNoOvulation(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, IrregularCycle: true}
	// 12, 28 and 40 days, the running cycle starting 2026-05-01.
	logs := cycleStartLogs(t, "2026-02-10", "2026-02-22", "2026-03-22", "2026-05-01")
	now := mustParseDay(t, "2026-05-03")

	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if stats.MinCycleLength != 12 || stats.MaxCycleLength != 40 {
		t.Fatalf("fixture: min/max = %d/%d, want 12/40", stats.MinCycleLength, stats.MaxCycleLength)
	}
	if PredictCycleWindow(stats.LastPeriodStart, 12, 14).Calculable {
		t.Fatal("fixture: the shortest cycle must be one no ovulation can be placed in")
	}
	if got := CalendarDayKey(stats.FertilityWindowStart); got != "2026-05-01" {
		t.Fatalf("window start = %s, want the cycle start 2026-05-01", got)
	}
	if got, want := CalendarDayKey(stats.FertilityWindowEnd), CalendarDayKey(PredictCycleWindow(stats.LastPeriodStart, 40, 14).OvulationDate); got != want {
		t.Fatalf("window end = %s, want the longest cycle's ovulation %s", got, want)
	}
	if stats.CurrentFertility != FertilityStatusFertile {
		t.Fatalf("status = %q on cycle day 3, want fertile", stats.CurrentFertility)
	}
}

// TestIrregularWindowIsWithheldWholeWhenItsLastDayIsUnspellable: the widened
// window ends on the longest cycle's ovulation, so that is the day which must
// still have a four-digit year; past it the window goes as a whole, as an
// unspellable median window does, and the status with it.
func TestIrregularWindowIsWithheldWholeWhenItsLastDayIsUnspellable(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, IrregularCycle: true}
	// 28, 28 and 60 days, the running cycle starting 9999-12-01: the median
	// ovulation (9999-12-14) is spellable, the longest cycle's is in 10000.
	logs := cycleStartLogs(t, "9999-08-07", "9999-09-04", "9999-10-02", "9999-12-01")
	now := mustParseDay(t, "9999-12-05")

	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if !projectedDay(PredictCycleWindow(stats.LastPeriodStart, stats.MedianCycleLength, 14).OvulationDate).Equal(mustParseDay(t, "9999-12-14")) {
		t.Fatal("fixture: the median ovulation must be spellable, or this is the median window's own case")
	}
	if !stats.FertilityWindowStart.IsZero() || !stats.FertilityWindowEnd.IsZero() || !stats.OvulationDate.IsZero() {
		t.Fatalf("window %s..%s ovulation %s, want all withheld", CalendarDayKey(stats.FertilityWindowStart), CalendarDayKey(stats.FertilityWindowEnd), CalendarDayKey(stats.OvulationDate))
	}
	if stats.OvulationImpossible {
		t.Fatal("an unspellable window is not an impossible ovulation")
	}
	if stats.CurrentFertility != FertilityStatusUnknown || stats.FertilityBasis != "" {
		t.Fatalf("status %q basis %q, want unknown with no basis", stats.CurrentFertility, stats.FertilityBasis)
	}
}

// staleBandFixture is two 28-day cycles (starts 2026-04-05, 05-03, 05-31) and
// today 2026-06-30, cycle day 31: past the 28-day reference, so both owner
// pages show the data as out of date, and not yet overdue, which needs a day
// past 35. The projection still places ovulation on 06-13 and the next period
// on 06-28, both behind today.
func staleBandFixture(t *testing.T) (*models.User, []models.DailyLog, time.Time) {
	t.Helper()

	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5}
	return user, cycleStartLogs(t, "2026-03-08", "2026-04-05", "2026-05-03", "2026-05-31"), mustParseDay(t, "2026-06-30")
}

func TestPublishedStatsWithholdsPhaseAndStatusOnOutOfDateData(t *testing.T) {
	user, logs, today := staleBandFixture(t)
	stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
	if stats.CurrentCycleDay != 31 || DashboardCycleOverdue(user, stats) {
		t.Fatalf("fixture: cycle day %d overdue %v, want day 31 and not overdue", stats.CurrentCycleDay, DashboardCycleOverdue(user, stats))
	}
	if !BuildDashboardCycleContext(user, logs, stats, today, time.UTC).CycleDataStale {
		t.Fatal("fixture: the dashboard must call this cycle's data out of date")
	}
	if stats.CurrentFertility == FertilityStatusUnknown || stats.CurrentPhase == "unknown" {
		t.Fatalf("fixture: the unpublished stats must carry a phase and a status to withhold, got %q/%q", stats.CurrentPhase, stats.CurrentFertility)
	}

	published, suppression, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)

	if suppression.PredictionsSuppressed || suppression.FertilitySuppressed || len(suppression.Reasons) != 0 {
		t.Fatalf("suppression = %+v — staleness is not a suppression signal", suppression)
	}
	if !published.CycleDataStale {
		t.Fatal("CycleDataStale = false, want the pages' verdict")
	}
	if published.CurrentFertility != FertilityStatusUnknown || published.FertilityBasis != "" {
		t.Fatalf("status %q basis %q, want unknown with no basis on out-of-date data", published.CurrentFertility, published.FertilityBasis)
	}
	if published.CurrentPhase != "unknown" {
		t.Fatalf("phase = %q, want unknown on out-of-date data", published.CurrentPhase)
	}
	// The dates stay, as they do on the pages beside the out-of-date banner.
	if published.NextPeriodStart.IsZero() || published.OvulationDate.IsZero() || published.FertilityWindowStart.IsZero() {
		t.Fatal("a projected date was withheld — staleness withholds the phase and the status only")
	}
}

// TestOutOfDateDataOutranksALoggedBleedingDay: the recomputed phase would say
// "menstrual" for a bleeding day logged without a new cycle start, and the
// pages still print "unknown" — so does the published copy.
func TestOutOfDateDataOutranksALoggedBleedingDay(t *testing.T) {
	user, logs, today := staleBandFixture(t)
	logs = append(logs, models.DailyLog{Date: today, IsPeriod: true, Flow: models.FlowLight})
	stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)

	published, _ := PublishedStats(user, stats, logs, today, time.UTC)
	if !published.CycleDataStale {
		t.Fatal("fixture: the logged day must not end the out-of-date state")
	}
	if got := DetectCurrentPhase(published, logs, today, time.UTC); got != "menstrual" {
		t.Fatalf("fixture: the recomputed phase = %q, want menstrual, or the override below is untested", got)
	}
	if published.CurrentPhase != "unknown" {
		t.Fatalf("phase = %q, want unknown — the out-of-date verdict wins", published.CurrentPhase)
	}
}

// TestOutOfDateVerdictFollowsThePagesInAPause: the pages resolve the verdict
// only where they publish a projection at all, so a paused account whose
// anchor is past its reference length is not called out of date.
func TestOutOfDateVerdictFollowsThePagesInAPause(t *testing.T) {
	user, logs, today := staleBandFixture(t)
	logs = append(logs, models.DailyLog{Date: mustParseDay(t, "2026-06-20"), PregnancyTest: models.PregnancyTestPositive})
	stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
	if !stats.PregnancyPaused {
		t.Fatal("fixture: the positive test must pause predictions")
	}
	if !DashboardCycleDataLooksStale(DashboardCycleStaleAnchor(user, stats, time.UTC), today, DashboardCycleReferenceLength(user, stats)) {
		t.Fatal("fixture: the anchor must be past the reference length, or the pause decides nothing here")
	}

	published, _ := PublishedStats(user, stats, logs, today, time.UTC)
	if published.CycleDataStale {
		t.Fatal("CycleDataStale = true during a pause, where neither page resolves it")
	}
	if published.CurrentFertility != FertilityStatusUnknown || published.FertilityBasis != "" {
		t.Fatalf("status %q basis %q, want the pause's unknown with no basis", published.CurrentFertility, published.FertilityBasis)
	}
}

// TestPublishedStatsKeepsAProjectedStatusOnCurrentData is the positive control:
// a regular account mid-cycle, inside its reference length, keeps its status,
// its projection basis and its phase.
func TestPublishedStatsKeepsAProjectedStatusOnCurrentData(t *testing.T) {
	user, logs, _ := staleBandFixture(t)

	for _, tc := range []struct {
		today      string
		wantStatus string
		wantPhase  string
	}{
		{"2026-06-10", FertilityStatusFertile, "follicular"},
		{"2026-06-20", FertilityStatusOutsideEstimatedWindow, "luteal"},
	} {
		today := mustParseDay(t, tc.today)
		stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
		published, _, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
		if published.CycleDataStale {
			t.Fatalf("%s: CycleDataStale on cycle day %d", tc.today, stats.CurrentCycleDay)
		}
		if published.CurrentFertility != tc.wantStatus || published.FertilityBasis != FertilityBasisProjection || published.CurrentPhase != tc.wantPhase {
			t.Fatalf("%s: phase %q status %q basis %q, want %q %q on the projection", tc.today, published.CurrentPhase, published.CurrentFertility, published.FertilityBasis, tc.wantPhase, tc.wantStatus)
		}
	}
}
