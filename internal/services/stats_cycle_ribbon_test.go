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
