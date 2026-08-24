package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// statscycleribbonDay parses a YYYY-MM-DD into the UTC-midnight form a
// DailyLog.Date is stored in.
func statscycleribbonDay(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

// statscycleribbonCycle returns the logs of one cycle: a start day plus
// periodDays-1 further period days, so buildCompletedCycleSpans observes both
// the cycle boundary and the period length.
func statscycleribbonCycle(t *testing.T, start string, periodDays int) []models.DailyLog {
	t.Helper()
	startDay := statscycleribbonDay(t, start)
	logs := []models.DailyLog{{Date: startDay, IsPeriod: true, CycleStart: true}}
	for offset := 1; offset < periodDays; offset++ {
		logs = append(logs, models.DailyLog{Date: startDay.AddDate(0, 0, offset), IsPeriod: true})
	}
	return logs
}

func statscycleribbonOwner(showHistoricalPhases bool) *models.User {
	return &models.User{Role: models.RoleOwner, CycleLength: 28, ShowHistoricalPhases: showHistoricalPhases}
}

// statscycleribbonHistory is four completed cycles of 30, 26, 33 and 28 days,
// plus the running one that closes the last of them.
func statscycleribbonHistory(t *testing.T) []models.DailyLog {
	t.Helper()
	logs := []models.DailyLog{}
	for _, cycle := range []struct {
		start  string
		period int
	}{
		{"2026-01-01", 5},
		{"2026-01-31", 4},
		{"2026-02-26", 5},
		{"2026-03-31", 6},
		{"2026-04-28", 5},
	} {
		logs = append(logs, statscycleribbonCycle(t, cycle.start, cycle.period)...)
	}
	return logs
}

func TestBuildStatsCycleRibbonWaitsForTwoCompletedCycles(t *testing.T) {
	logs := statscycleribbonCycle(t, "2026-01-01", 5)
	logs = append(logs, statscycleribbonCycle(t, "2026-01-31", 5)...)

	single := buildStatsCycleRibbon(
		statscycleribbonOwner(false),
		CycleStats{LutealPhase: 14},
		logs,
		buildCompletedCycleSpans(logs, time.UTC),
	)
	if single.Visible {
		t.Fatal("one completed cycle is not a comparison — the stack must stay hidden")
	}

	logs = append(logs, statscycleribbonCycle(t, "2026-02-26", 5)...)
	pair := buildStatsCycleRibbon(
		statscycleribbonOwner(false),
		CycleStats{LutealPhase: 14},
		logs,
		buildCompletedCycleSpans(logs, time.UTC),
	)
	if !pair.Visible {
		t.Fatal("two completed cycles are the basic-insights tier and must render")
	}
	if len(pair.Rows) != 2 {
		t.Fatalf("expected two rows, got %d", len(pair.Rows))
	}
}

// TestBuildStatsCycleRibbonSharesOneAxisAcrossRows is the point of the stack: a
// longer cycle has to LOOK longer, which it only does when every row is drawn
// against the longest one and the surplus cells stay out of the cycle.
func TestBuildStatsCycleRibbonSharesOneAxisAcrossRows(t *testing.T) {
	logs := statscycleribbonHistory(t)
	ribbon := buildStatsCycleRibbon(
		statscycleribbonOwner(false),
		CycleStats{LutealPhase: 14},
		logs,
		buildCompletedCycleSpans(logs, time.UTC),
	)
	if !ribbon.Visible {
		t.Fatal("expected a visible stack for four completed cycles")
	}
	if ribbon.AxisDays != 33 {
		t.Fatalf("the axis is the longest cycle in the stack (33), got %d", ribbon.AxisDays)
	}
	if len(ribbon.Rows) != 4 {
		t.Fatalf("expected four rows, got %d", len(ribbon.Rows))
	}

	wantLengths := []int{30, 26, 33, 28}
	for index, row := range ribbon.Rows {
		if row.CycleLength != wantLengths[index] {
			t.Fatalf("row %d: expected cycle length %d, got %d", index, wantLengths[index], row.CycleLength)
		}
		if len(row.Days) != ribbon.AxisDays {
			t.Fatalf("row %d: every row carries the whole axis, got %d cells of %d", index, len(row.Days), ribbon.AxisDays)
		}

		inCycle := 0
		for _, day := range row.Days {
			if day.InCycle {
				inCycle++
			}
		}
		if inCycle != row.CycleLength {
			t.Fatalf("row %d: expected %d cells inside the cycle, got %d", index, row.CycleLength, inCycle)
		}
		if !row.Days[row.CycleLength-1].InCycle {
			t.Fatalf("row %d: the last day of the cycle must be inside it", index)
		}
		// The longest row fills the axis and has no day past its own end.
		if row.CycleLength < ribbon.AxisDays && row.Days[row.CycleLength].InCycle {
			t.Fatalf("row %d: the cycle must end exactly at day %d", index, row.CycleLength)
		}
	}
}

// TestBuildStatsCycleRibbonKeepsOnlyTheMostRecentCycles pins which four of a
// longer history are shown: the last ones, not the first.
func TestBuildStatsCycleRibbonKeepsOnlyTheMostRecentCycles(t *testing.T) {
	logs := statscycleribbonHistory(t)
	logs = append(logs, statscycleribbonCycle(t, "2026-05-26", 5)...)

	ribbon := buildStatsCycleRibbon(
		statscycleribbonOwner(false),
		CycleStats{LutealPhase: 14},
		logs,
		buildCompletedCycleSpans(logs, time.UTC),
	)
	if len(ribbon.Rows) != statsCycleRibbonRows {
		t.Fatalf("expected %d rows, got %d", statsCycleRibbonRows, len(ribbon.Rows))
	}
	if got := ribbon.Rows[0].Start.Format("2006-01-02"); got != "2026-01-31" {
		t.Fatalf("expected the stack to drop the oldest cycle, first row starts %s", got)
	}
	if got := ribbon.Rows[3].Start.Format("2006-01-02"); got != "2026-04-28" {
		t.Fatalf("expected the newest completed cycle last, got %s", got)
	}
}

// TestBuildStatsCycleRibbonDrawsObservedPeriodDaysWithoutInferredPhases is the
// honesty boundary of the stack: with ShowHistoricalPhases off, a past cycle
// shows what was RECORDED — its length and its period days — and nothing
// inferred. That preference is the calendar's own, not a second switch.
func TestBuildStatsCycleRibbonDrawsObservedPeriodDaysWithoutInferredPhases(t *testing.T) {
	logs := statscycleribbonHistory(t)
	ribbon := buildStatsCycleRibbon(
		statscycleribbonOwner(false),
		CycleStats{LutealPhase: 14},
		logs,
		buildCompletedCycleSpans(logs, time.UTC),
	)
	if ribbon.ShowPhases {
		t.Fatal("did not expect inferred phases with ShowHistoricalPhases off")
	}

	// Row 0 is the 2026-01-01 cycle: five observed period days.
	row := ribbon.Rows[0]
	if row.PeriodLength != 5 {
		t.Fatalf("expected five observed period days, got %d", row.PeriodLength)
	}
	for _, day := range row.Days {
		switch {
		case day.Day <= 5:
			if !day.IsPeriod || day.Phase != "menstrual" {
				t.Fatalf("day %d is a recorded period day: period=%v phase=%q", day.Day, day.IsPeriod, day.Phase)
			}
		case day.InCycle:
			if day.Phase != "" {
				t.Fatalf("day %d carries the inferred phase %q with the preference off", day.Day, day.Phase)
			}
		}
		if day.IsFertile || day.IsFertilePeak {
			t.Fatalf("day %d shades a fertile window inferred for a past cycle", day.Day)
		}
	}
}

// TestBuildStatsCycleRibbonInfersPhasesOnlyWhenTheOwnerAsked is the other half:
// with the preference on, the same past cycle carries the phase map and the
// fertile window computed for its own observed length.
func TestBuildStatsCycleRibbonInfersPhasesOnlyWhenTheOwnerAsked(t *testing.T) {
	logs := statscycleribbonHistory(t)
	ribbon := buildStatsCycleRibbon(
		statscycleribbonOwner(true),
		CycleStats{LutealPhase: 14},
		logs,
		buildCompletedCycleSpans(logs, time.UTC),
	)
	if !ribbon.ShowPhases {
		t.Fatal("expected inferred phases with ShowHistoricalPhases on")
	}

	// Row 0: a 30-day cycle with a 14-day luteal phase → ovulation on day 16.
	row := ribbon.Rows[0]
	phases := map[string]int{}
	peakDays := []int{}
	for _, day := range row.Days {
		if !day.InCycle {
			continue
		}
		phases[day.Phase]++
		if day.IsFertilePeak {
			peakDays = append(peakDays, day.Day)
		}
	}

	if phases["menstrual"] != 5 {
		t.Fatalf("menstrual stays the OBSERVED period length, got %d days", phases["menstrual"])
	}
	if phases["ovulation"] != 1 {
		t.Fatalf("expected exactly one ovulation day, got %d", phases["ovulation"])
	}
	if len(peakDays) != 1 || peakDays[0] != 16 {
		t.Fatalf("expected the fertile peak on day 16 of a 30-day cycle, got %v", peakDays)
	}
	if phases["luteal"] != 14 {
		t.Fatalf("expected 14 luteal days after ovulation on day 16 of 30, got %d", phases["luteal"])
	}
}

// TestStatsCycleRibbonAxisDaysClampsAWildCycle pins the DOM bound. A cycle
// merged by a missed period log is arbitrarily long — the axis stops at the
// cap rather than emitting a cell per day of it, on every row of the stack.
func TestStatsCycleRibbonAxisDaysClampsAWildCycle(t *testing.T) {
	within := statsCycleRibbonAxisDays([]completedCycleSpan{{CycleLength: 26}, {CycleLength: 34}})
	if within != 34 {
		t.Fatalf("the axis is the longest cycle, got %d", within)
	}

	clamped := statsCycleRibbonAxisDays([]completedCycleSpan{
		{CycleLength: 28},
		{CycleLength: statsCycleRibbonMaxAxisDays + 40},
	})
	if clamped != statsCycleRibbonMaxAxisDays {
		t.Fatalf("expected the axis clamped to %d, got %d", statsCycleRibbonMaxAxisDays, clamped)
	}
}

// TestBuildStatsCycleRibbonRefusesAnEmptyAxis pins the guard that keeps a
// degenerate span out of the render path. buildCompletedCycleSpans drops a
// non-positive cycle length before it ever gets here, so this is defence in
// depth against a future second producer of spans — and it is asserted rather
// than assumed, because the failure it prevents is a stack of rows with no
// cells, which reads as an empty card rather than as a bug.
func TestBuildStatsCycleRibbonRefusesAnEmptyAxis(t *testing.T) {
	ribbon := buildStatsCycleRibbon(
		statscycleribbonOwner(false),
		CycleStats{LutealPhase: 14},
		nil,
		[]completedCycleSpan{{CycleLength: 0}, {CycleLength: 0}},
	)
	if ribbon.Visible {
		t.Fatal("expected no stack when no span carries a positive length")
	}
	if len(ribbon.Rows) != 0 {
		t.Fatalf("expected no rows, got %d", len(ribbon.Rows))
	}
}

// TestStatsCycleRibbonPhaseFallsBackWhenNoWindowIsCalculable covers the cycle
// too short for PredictCycleWindow to place an ovulation day in it. The days
// after the period are then simply follicular: the row must not claim an
// ovulation it cannot locate, and must not leave the phase blank while the
// owner has inferred phases switched on.
func TestStatsCycleRibbonPhaseFallsBackWhenNoWindowIsCalculable(t *testing.T) {
	cycleStart := statscycleribbonDay(t, "2026-01-01")
	window := PredictCycleWindow(cycleStart, 6, 14)
	if window.Calculable {
		t.Fatal("expected a 6-day cycle with a 14-day luteal phase to have no calculable window")
	}

	if got := statsCycleRibbonPhase(2, 3, window, cycleStart); got != "menstrual" {
		t.Fatalf("day 2 of a 3-day period is menstrual, got %q", got)
	}
	if got := statsCycleRibbonPhase(5, 3, window, cycleStart); got != "follicular" {
		t.Fatalf("expected the follicular fallback with no window, got %q", got)
	}
}

func TestBuildStatsCycleRibbonMarksRecordedDays(t *testing.T) {
	logs := statscycleribbonHistory(t)
	// A note on day 12 of the first cycle: an entry with data, not a period day.
	logs = append(logs, models.DailyLog{
		Date:  statscycleribbonDay(t, "2026-01-12"),
		Notes: "headache",
	})
	// An empty entry on day 20 — a row in the table with nothing recorded in it.
	logs = append(logs, models.DailyLog{Date: statscycleribbonDay(t, "2026-01-20")})

	ribbon := buildStatsCycleRibbon(
		statscycleribbonOwner(false),
		CycleStats{LutealPhase: 14},
		logs,
		buildCompletedCycleSpans(logs, time.UTC),
	)

	logged := []int{}
	for _, day := range ribbon.Rows[0].Days {
		if day.IsLogged {
			logged = append(logged, day.Day)
		}
	}
	// Days 1-5 are the recorded period, day 12 the note; day 20 holds nothing.
	want := []int{1, 2, 3, 4, 5, 12}
	if len(logged) != len(want) {
		t.Fatalf("expected logged days %v, got %v", want, logged)
	}
	for index, day := range want {
		if logged[index] != day {
			t.Fatalf("expected logged days %v, got %v", want, logged)
		}
	}
}

// statscycleribbonInferredFertility counts, across the whole stack, the cells
// that make a fertility claim: a shaded fertile day, the peak band, and an
// ovulation phase cell. It is the quantity the medical-safety suppression has
// to drive to zero — three encodings of one claim, so a guard reading only one
// of them would go green while the other two still paint.
func statscycleribbonInferredFertility(ribbon StatsCycleRibbon) (fertile int, peak int, ovulation int) {
	for _, row := range ribbon.Rows {
		for _, day := range row.Days {
			if day.IsFertile {
				fertile++
			}
			if day.IsFertilePeak {
				peak++
			}
			if day.Phase == "ovulation" {
				ovulation++
			}
		}
	}
	return fertile, peak, ovulation
}

// TestBuildStatsCycleRibbonSuppressesInferredFertilityInUnpredictableMode is the
// medical-safety gate on this surface. An owner in unpredictable-cycle mode has
// said the cycle math does not describe her, and every other projected surface
// answers by withholding the fertile window, the peak band and the ovulation
// day — the calendar included, whose historical-phase pass sits BELOW the same
// suppression return, so inferred history is not exempt there. The stack shaded
// all three anyway, on the strength of the ShowHistoricalPhases preference
// alone: a fertility claim at a confidence the data no longer carries.
//
// The case is named for unpredictable mode, which is the disjunct it drives; the
// gate itself is the shared PredictionsSuppressed predicate, so a pregnancy
// pause and an overdue cycle reach this surface through the same line and are
// pinned on the predicate rather than re-pinned per surface here.
//
// The suppressed half is a negative assertion, so the same history renders once
// with prediction enabled first: that anchor proves the three claims can appear
// here at all, and that the guard is not green against an empty stack. What must
// SURVIVE the gate is asserted too — the rows, their observed lengths and the
// recorded period days are facts, not projections.
func TestBuildStatsCycleRibbonSuppressesInferredFertilityInUnpredictableMode(t *testing.T) {
	logs := statscycleribbonHistory(t)
	spans := buildCompletedCycleSpans(logs, time.UTC)

	predicting := buildStatsCycleRibbon(
		statscycleribbonOwner(true),
		CycleStats{LutealPhase: 14},
		logs,
		spans,
	)
	fertile, peak, ovulation := statscycleribbonInferredFertility(predicting)
	if fertile == 0 || peak == 0 || ovulation == 0 {
		t.Fatalf("anchor: with prediction on, the stack shades a fertile window, a peak and an ovulation day; got fertile=%d peak=%d ovulation=%d", fertile, peak, ovulation)
	}

	owner := statscycleribbonOwner(true)
	owner.UnpredictableCycle = true
	ribbon := buildStatsCycleRibbon(
		owner,
		CycleStats{LutealPhase: 14},
		logs,
		spans,
	)

	if !ribbon.Visible || len(ribbon.Rows) != len(predicting.Rows) {
		t.Fatalf("recorded cycles are facts and keep rendering: visible=%v rows=%d", ribbon.Visible, len(ribbon.Rows))
	}
	fertile, peak, ovulation = statscycleribbonInferredFertility(ribbon)
	if fertile != 0 || peak != 0 || ovulation != 0 {
		t.Fatalf("unpredictable mode suppresses every fertility claim on the stack; got fertile=%d peak=%d ovulation=%d", fertile, peak, ovulation)
	}

	// Row 0 is the 2026-01-01 cycle: five recorded period days, which stay.
	row := ribbon.Rows[0]
	if row.CycleLength != 30 || row.PeriodLength != 5 {
		t.Fatalf("expected the observed 30-day cycle with five period days, got %d/%d", row.CycleLength, row.PeriodLength)
	}
	for _, day := range row.Days {
		if day.Day <= row.PeriodLength && (!day.IsPeriod || day.Phase != "menstrual") {
			t.Fatalf("day %d is a recorded period day: period=%v phase=%q", day.Day, day.IsPeriod, day.Phase)
		}
	}
}

// statscycleribbonStack builds a stack from consecutive cycle starts: every
// entry is one cycle's first day plus its period days, and the last start only
// closes the cycle before it. Lengths therefore come out as the gaps between
// the starts, which is what the ribbon draws.
func statscycleribbonStack(t *testing.T, cycles []statscycleribbonSpec) []models.DailyLog {
	t.Helper()
	logs := []models.DailyLog{}
	for _, cycle := range cycles {
		logs = append(logs, statscycleribbonCycle(t, cycle.start, cycle.period)...)
	}
	return logs
}

type statscycleribbonSpec struct {
	start  string
	period int
}

// statscycleribbonRowFertility counts one row's three encodings of a fertility
// claim, the same three the stack-wide helper counts.
func statscycleribbonRowFertility(row StatsCycleRibbonRow) (fertile int, peak int, ovulation int) {
	for _, day := range row.Days {
		if day.IsFertile {
			fertile++
		}
		if day.IsFertilePeak {
			peak++
		}
		if day.Phase == "ovulation" {
			ovulation++
		}
	}
	return fertile, peak, ovulation
}

// TestBuildStatsCycleRibbonSuppressesAClampedOvulationEstimate is the medical-
// safety floor on this surface. PredictCycleWindow reports OvulationExact=false
// when the luteal phase had to be SHORTENED to fit the cycle at all — every
// completed cycle under 19 days — so the ovulation day it returns is a fallback
// the data does not carry rather than an estimate of it. Display confidence
// follows data confidence: the row keeps its recorded length and its recorded
// period days and drops every mark derived from that fallback. The dashboard
// says "approximate" for the same value; here the cells are colour with no
// wording to qualify them, so suppression is the floor.
//
// The stack is drawn with a cycle whose window IS exact beside it: that anchor
// proves the marks can appear at all, so the negative half cannot pass against
// an empty ribbon.
func TestBuildStatsCycleRibbonSuppressesAClampedOvulationEstimate(t *testing.T) {
	// 15 days (Jan 1 → Jan 16), then 30 (Jan 16 → Feb 15). A 15-day cycle with
	// a 14-day luteal phase clamps the phase to 10, i.e. OvulationExact=false.
	logs := statscycleribbonStack(t, []statscycleribbonSpec{
		{"2026-01-01", 5},
		{"2026-01-16", 5},
		{"2026-02-15", 5},
	})

	if _, exact := CalcOvulationDay(15, 14); exact {
		t.Fatal("premise: a 15-day cycle with a 14-day luteal phase must clamp, i.e. report exact=false")
	}
	if _, exact := CalcOvulationDay(30, 14); !exact {
		t.Fatal("anchor premise: a 30-day cycle with a 14-day luteal phase must fit exactly")
	}

	ribbon := buildStatsCycleRibbon(
		statscycleribbonOwner(true),
		CycleStats{LutealPhase: 14},
		logs,
		buildCompletedCycleSpans(logs, time.UTC),
	)
	if !ribbon.ShowPhases || len(ribbon.Rows) != 2 {
		t.Fatalf("expected two rows with inferred phases on, got rows=%d showPhases=%v", len(ribbon.Rows), ribbon.ShowPhases)
	}

	clamped, fitted := ribbon.Rows[0], ribbon.Rows[1]
	if clamped.CycleLength != 15 || fitted.CycleLength != 30 {
		t.Fatalf("expected a 15-day row then a 30-day row, got %d and %d", clamped.CycleLength, fitted.CycleLength)
	}

	anchorFertile, anchorPeak, anchorOvulation := statscycleribbonRowFertility(fitted)
	if anchorFertile == 0 || anchorPeak != 1 || anchorOvulation != 1 {
		t.Fatalf("anchor: the 30-day row's window is exact and must carry the marks; got fertile=%d peak=%d ovulation=%d", anchorFertile, anchorPeak, anchorOvulation)
	}

	fertile, peak, ovulation := statscycleribbonRowFertility(clamped)
	if fertile != 0 || peak != 0 || ovulation != 0 {
		t.Fatalf("a clamped ovulation day is a fallback, not an observation: the 15-day row must carry no fertility claim; got fertile=%d peak=%d ovulation=%d", fertile, peak, ovulation)
	}

	// What must survive the floor: the recorded length and the period days.
	inCycle := 0
	for _, day := range clamped.Days {
		if day.InCycle {
			inCycle++
		}
		if day.Day <= clamped.PeriodLength && (!day.IsPeriod || day.Phase != "menstrual") {
			t.Fatalf("day %d is a recorded period day: period=%v phase=%q", day.Day, day.IsPeriod, day.Phase)
		}
	}
	if inCycle != 15 {
		t.Fatalf("the recorded 15-day length is a fact and must still be drawn, got %d cells in cycle", inCycle)
	}
}

// TestBuildStatsCycleRibbonNeverMarksAPeakOutsideTheOvulationPhase pins the two
// axes of one cell against each other. Phase and fertility are orthogonal by
// design (#416) and a fertile day may legitimately overlap the period — but the
// peak IS the ovulation day, and the phase taxonomy spends its "ovulation"
// value on the menstrual branch whenever the two land on the same day. A
// 21-day cycle with a 7-day period does exactly that: the window is exact and
// puts ovulation on day 7, which is also the last recorded period day. The cell
// then said "menstrual" and "fertile peak" at once, and which of the two an
// owner saw was decided by CSS precedence rather than by this service.
func TestBuildStatsCycleRibbonNeverMarksAPeakOutsideTheOvulationPhase(t *testing.T) {
	logs := statscycleribbonStack(t, []statscycleribbonSpec{
		{"2026-01-01", 7},
		{"2026-01-22", 7},
		{"2026-02-19", 7},
	})

	ovulationDay, exact := CalcOvulationDay(21, 14)
	if !exact || ovulationDay != 7 {
		t.Fatalf("premise: a 21-day cycle must place an EXACT ovulation on day 7, got day=%d exact=%v", ovulationDay, exact)
	}

	ribbon := buildStatsCycleRibbon(
		statscycleribbonOwner(true),
		CycleStats{LutealPhase: 14},
		logs,
		buildCompletedCycleSpans(logs, time.UTC),
	)
	if !ribbon.ShowPhases {
		t.Fatal("expected inferred phases on")
	}

	peaks := 0
	for rowIndex, row := range ribbon.Rows {
		for _, day := range row.Days {
			if !day.IsFertilePeak {
				continue
			}
			peaks++
			if day.Phase != "ovulation" {
				t.Fatalf("row %d day %d is marked the fertile peak while its phase says %q — one cell making two contradicting claims", rowIndex, day.Day, day.Phase)
			}
		}
	}
	if peaks == 0 {
		t.Fatal("anchor: the stack must still mark a peak somewhere, or this guard is green against an empty ribbon")
	}
}

// TestBuildStatsCycleRibbonFlagsARowTheAxisCannotHold covers the silent half of
// the axis cap. The cap is the DOM bound and stays; what must not stay is a row
// claiming to end where it was merely cut off. Two cycles past the cap were
// drawn as two identical full-width rows, so the comparison the stack exists
// for reported them equal while the number beside each row said otherwise.
func TestBuildStatsCycleRibbonFlagsARowTheAxisCannotHold(t *testing.T) {
	within := buildStatsCycleRibbon(
		statscycleribbonOwner(false),
		CycleStats{LutealPhase: 14},
		nil,
		[]completedCycleSpan{
			{Start: statscycleribbonDay(t, "2026-01-01"), CycleLength: 30, PeriodLength: 5},
			{Start: statscycleribbonDay(t, "2026-01-31"), CycleLength: 34, PeriodLength: 5},
		},
	)
	for index, row := range within.Rows {
		if row.Truncated {
			t.Fatalf("row %d fits the axis (%d of %d) and must not be flagged truncated", index, row.CycleLength, within.AxisDays)
		}
	}
	if within.AxisTruncated {
		t.Fatal("a stack inside the cap draws every cycle whole")
	}

	beyond := buildStatsCycleRibbon(
		statscycleribbonOwner(false),
		CycleStats{LutealPhase: 14},
		nil,
		[]completedCycleSpan{
			{Start: statscycleribbonDay(t, "2026-01-01"), CycleLength: statsCycleRibbonMaxAxisDays + 15, PeriodLength: 5},
			{Start: statscycleribbonDay(t, "2026-04-01"), CycleLength: statsCycleRibbonMaxAxisDays + 30, PeriodLength: 5},
		},
	)
	if beyond.AxisDays != statsCycleRibbonMaxAxisDays {
		t.Fatalf("the axis stays capped at %d, got %d", statsCycleRibbonMaxAxisDays, beyond.AxisDays)
	}
	if !beyond.AxisTruncated {
		t.Fatal("the stack must report that the axis could not hold every cycle")
	}
	for index, row := range beyond.Rows {
		if !row.Truncated {
			t.Fatalf("row %d is %d days long against a %d-day axis and must say so", index, row.CycleLength, beyond.AxisDays)
		}
	}
}
