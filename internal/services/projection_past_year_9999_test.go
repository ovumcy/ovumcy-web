package services

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-108: a date derived from a late-9999 day is answered as absent once it
// would fall after 9999-12-31, the last day a four-digit year can spell. Day
// inputs stop at DayDateMax (9999-12-30), so these fixtures log their last cycle
// start inside the accepted range and let the projection cross the year.

// lateYear9999Now is the last day the day routes accept, used as the owner's
// today: every fixture below has its cycle starts on or before it.
const lateYear9999Now = "9999-12-30"

func lateYear9999Owner() *models.User {
	return &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
}

// lateYear9999Starts logs three explicit cycle starts 28 days apart, the last
// one on lastStart.
func lateYear9999Starts(t *testing.T, lastStart string) []models.DailyLog {
	t.Helper()
	last := mustParseDashboardDay(t, lastStart)
	logs := make([]models.DailyLog, 0, 4)
	for _, back := range []int{84, 56, 28, 0} {
		logs = append(logs, models.DailyLog{Date: last.AddDate(0, 0, -back), IsPeriod: true, CycleStart: true})
	}
	return logs
}

type lateYear9999Case struct {
	name      string
	lastStart string
	// wantOvulation is the ovulation day the case still publishes, or "" when the
	// window crosses 9999-12-31 and is withheld as a whole.
	wantOvulation string
}

// Both cases put the next period start past the year (lastStart + 28). The
// first also puts the ovulation there (lastStart + 13 = 10000-01-02); the
// second keeps it inside (9999-12-23), so a guard that withheld the window
// whenever the NEXT start crossed would red it.
func lateYear9999Cases() []lateYear9999Case {
	return []lateYear9999Case{
		{name: "whole projection past the year", lastStart: "9999-12-20"},
		{name: "ovulation inside the year, next start past it", lastStart: "9999-12-10", wantOvulation: "9999-12-23"},
	}
}

func assertLateYear9999Stats(t *testing.T, stats CycleStats, tc lateYear9999Case) {
	t.Helper()
	if !stats.NextPeriodStart.IsZero() {
		t.Fatalf("NextPeriodStart = %s, want absent: lastStart %s + 28 days falls after 9999-12-31", stats.NextPeriodStart.Format("2006-01-02"), tc.lastStart)
	}
	if stats.OvulationImpossible {
		t.Fatal("OvulationImpossible = true: a window past the year is unnamed, not impossible")
	}
	if tc.wantOvulation == "" {
		for field, value := range map[string]time.Time{
			"OvulationDate":        stats.OvulationDate,
			"FertilityWindowStart": stats.FertilityWindowStart,
			"FertilityWindowEnd":   stats.FertilityWindowEnd,
		} {
			if !value.IsZero() {
				t.Fatalf("%s = %s, want absent: the window ends after 9999-12-31", field, value.Format("2006-01-02"))
			}
		}
		if stats.OvulationExact {
			t.Fatal("OvulationExact = true beside an absent ovulation date")
		}
		return
	}
	if got := CalendarDayKey(stats.OvulationDate); got != tc.wantOvulation {
		t.Fatalf("OvulationDate = %q, want %q: a window inside the year is published", got, tc.wantOvulation)
	}
}

func TestBuildCycleStatsAnswersAProjectionPastYear9999AsAbsent(t *testing.T) {
	t.Parallel()
	for _, tc := range lateYear9999Cases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stats := BuildCycleStats(lateYear9999Starts(t, tc.lastStart), mustParseDashboardDay(t, lateYear9999Now), BoundaryContext{})
			if got := CalendarDayKey(stats.LastPeriodStart); got != tc.lastStart {
				t.Fatalf("precondition: LastPeriodStart = %q, want %q", got, tc.lastStart)
			}
			assertLateYear9999Stats(t, stats, tc)
		})
	}
}

// ApplyUserCycleBaseline recomputes the projection on its own, so it is pinned
// against empty input stats: what it answers cannot be BuildCycleStats' guard.
func TestApplyUserCycleBaselineAnswersAProjectionPastYear9999AsAbsent(t *testing.T) {
	t.Parallel()
	for _, tc := range lateYear9999Cases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logs := lateYear9999Starts(t, tc.lastStart)
			stats := ApplyUserCycleBaseline(lateYear9999Owner(), logs, CycleStats{MedianCycleLength: 28}, mustParseDashboardDay(t, lateYear9999Now), time.UTC)
			if got := CalendarDayKey(stats.LastPeriodStart); got != tc.lastStart {
				t.Fatalf("precondition: LastPeriodStart = %q, want %q", got, tc.lastStart)
			}
			assertLateYear9999Stats(t, stats, tc)
		})
	}
}

func TestDashboardUpcomingPredictionsAnswersAProjectionPastYear9999AsAbsent(t *testing.T) {
	t.Parallel()
	// 9999-12-20 + 28 and its ovulation (+13) both cross the year; today is before
	// that ovulation, so the anchor is not rolled a cycle forward first.
	stats := CycleStats{LastPeriodStart: mustParseDashboardDay(t, "9999-12-20"), LutealPhase: 14}
	prediction := DashboardUpcomingPredictions(stats, lateYear9999Owner(), mustParseDashboardDay(t, lateYear9999Now), 28)
	if !prediction.NextPeriodStart.IsZero() {
		t.Fatalf("NextPeriodStart = %s, want absent", prediction.NextPeriodStart.Format("2006-01-02"))
	}
	if !prediction.OvulationDate.IsZero() {
		t.Fatalf("OvulationDate = %s, want absent", prediction.OvulationDate.Format("2006-01-02"))
	}
	if prediction.OvulationExact || prediction.OvulationImpossible {
		t.Fatalf("exact=%v impossible=%v, want both false beside an unnamed ovulation", prediction.OvulationExact, prediction.OvulationImpossible)
	}

	// Control: a projection that stays inside the year is untouched.
	inside := DashboardUpcomingPredictions(CycleStats{LastPeriodStart: mustParseDashboardDay(t, "9999-11-20"), LutealPhase: 14}, lateYear9999Owner(), mustParseDashboardDay(t, "9999-11-25"), 28)
	if got := CalendarDayKey(inside.NextPeriodStart); got != "9999-12-18" {
		t.Fatalf("control NextPeriodStart = %q, want 9999-12-18", got)
	}
	if got := CalendarDayKey(inside.OvulationDate); got != "9999-12-03" {
		t.Fatalf("control OvulationDate = %q, want 9999-12-03", got)
	}
}

func TestDashboardPredictionRangeIsAbsentWhenItsEndPassesYear9999(t *testing.T) {
	t.Parallel()
	regular := CycleStats{CompletedCycleCount: 3, CycleLengthStdDev: 2}
	irregularOwner := lateYear9999Owner()
	irregularOwner.IrregularCycle = true
	irregular := CycleStats{CompletedCycleCount: 3, MinCycleLength: 20, MaxCycleLength: 40, LastPeriodStart: mustParseDashboardDay(t, "9999-12-01")}

	for _, tc := range []struct {
		name      string
		user      *models.User
		stats     CycleStats
		predicted string
		wantEnd   string
	}{
		{name: "regular span ending on the last day", user: lateYear9999Owner(), stats: regular, predicted: "9999-12-29", wantEnd: "9999-12-31"},
		{name: "regular span ending past the year", user: lateYear9999Owner(), stats: regular, predicted: "9999-12-30"},
		{name: "irregular span ending past the year", user: irregularOwner, stats: irregular, predicted: "9999-12-29"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start, end, ok := DashboardPredictionRange(tc.user, tc.stats, mustParseDashboardDay(t, tc.predicted), time.UTC)
			if tc.wantEnd == "" {
				if ok || !start.IsZero() || !end.IsZero() {
					t.Fatalf("range = %s..%s ok=%v, want absent as a whole", CalendarDayKey(start), CalendarDayKey(end), ok)
				}
				return
			}
			if !ok || CalendarDayKey(end) != tc.wantEnd {
				t.Fatalf("range = %s..%s ok=%v, want it to end on %s", CalendarDayKey(start), CalendarDayKey(end), ok, tc.wantEnd)
			}
		})
	}
}

// The dashboard line names the predicted period's first and last day, so a band
// that starts inside the year and ends after it is withheld whole.
func TestDashboardNextPeriodBandIsAbsentWhenItsLastDayPassesYear9999(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		lastStart string
		wantStart string
		wantEnd   string
	}{
		{name: "band ending on the last day", lastStart: "9999-11-29", wantStart: "9999-12-27", wantEnd: "9999-12-31"},
		{name: "band starting on the last day", lastStart: "9999-12-03"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stats := CycleStats{LastPeriodStart: mustParseDashboardDay(t, tc.lastStart), MedianCycleLength: 28, AveragePeriodLength: 5, LutealPhase: 14}
			display := buildDashboardPredictionDisplay(lateYear9999Owner(), nil, stats, mustParseDashboardDay(t, "9999-12-26"), time.UTC)
			if got, want := CalendarDayKey(display.nextPeriodStart), tc.wantStart; got != want {
				t.Fatalf("nextPeriodStart = %q, want %q", got, want)
			}
			if got, want := CalendarDayKey(display.nextPeriodEnd), tc.wantEnd; got != want {
				t.Fatalf("nextPeriodEnd = %q, want %q", got, want)
			}
		})
	}
}

var icsDateValue = regexp.MustCompile(`^DT(?:START|END);VALUE=DATE:(.*)$`)

// icsDateValues returns every DTSTART/DTEND value in body.
func icsDateValues(body string) []string {
	values := make([]string, 0)
	for _, line := range strings.Split(body, "\r\n") {
		if match := icsDateValue.FindStringSubmatch(line); match != nil {
			values = append(values, match[1])
		}
	}
	return values
}

// An RFC 5545 DATE is YYYYMMDD. The feed's all-day event spells its exclusive
// end as the next day, so an event on 9999-12-31 already has no DTEND spelling
// and is left out, while one on 9999-12-30 ends on 9999-12-31 and stays.
func TestCalendarFeedLeavesOutAnEventWhoseEndPassesYear9999(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		lastStart string
		wantEvent string
	}{
		{name: "next start on 9999-12-30", lastStart: "9999-12-02", wantEvent: "99991230"},
		{name: "next start on 9999-12-31", lastStart: "9999-12-03"},
		{name: "every projection past the year", lastStart: "9999-12-20"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
				User:     lateYear9999Owner(),
				Logs:     lateYear9999Starts(t, tc.lastStart),
				Now:      mustParseDashboardDay(t, "9999-12-29"),
				Location: time.UTC,
			}))
			values := icsDateValues(body)
			for _, value := range values {
				if len(value) != 8 {
					t.Fatalf("DTSTART/DTEND value %q is not a YYYYMMDD date:\n%s", value, body)
				}
			}
			if tc.wantEvent == "" {
				if len(values) != 0 {
					t.Fatalf("want no event, got DTSTART/DTEND values %v:\n%s", values, body)
				}
				return
			}
			if !strings.Contains(body, "DTSTART;VALUE=DATE:"+tc.wantEvent+"\r\n") || !strings.Contains(body, "DTEND;VALUE=DATE:99991231\r\n") {
				t.Fatalf("want the event on %s ending 99991231 kept, got:\n%s", tc.wantEvent, body)
			}
		})
	}
}

// calendarProjectedFlags names the projected markers a grid cell carries.
func calendarProjectedFlags(day CalendarDayState) []string {
	flags := make([]string, 0)
	for name, set := range map[string]bool{
		"IsPredicted":            day.IsPredicted,
		"IsPredictedStartWindow": day.IsPredictedStartWindow,
		"IsPreFertile":           day.IsPreFertile,
		"IsFertilityEdge":        day.IsFertilityEdge,
		"IsFertilityPeak":        day.IsFertilityPeak,
		"IsOvulation":            day.IsOvulation,
		"IsTentativeOvulation":   day.IsTentativeOvulation,
	} {
		if set {
			flags = append(flags, name)
		}
	}
	return flags
}

// lateYear9999Periods logs five bleeding days from each of lateYear9999Starts,
// so the projected band is five days long and can reach past the year.
func lateYear9999Periods(t *testing.T, lastStart string) []models.DailyLog {
	t.Helper()
	starts := lateYear9999Starts(t, lastStart)
	logs := make([]models.DailyLog, 0, len(starts)*5)
	for _, start := range starts {
		logs = append(logs, start)
		for offset := 1; offset < 5; offset++ {
			logs = append(logs, models.DailyLog{Date: start.Date.AddDate(0, 0, offset), IsPeriod: true})
		}
	}
	return logs
}

func calendarWindowFlags(day CalendarDayState) bool {
	return day.IsPreFertile || day.IsFertilityEdge || day.IsFertilityPeak || day.IsOvulation || day.IsTentativeOvulation
}

// The December 9999 grid (Sunday week start) ends on 10000-01-01. A window whose
// ovulation crosses the year is withheld whole on the grid as in the stats —
// fertile days, ovulation and the pre-fertile lead-in — and no projected marker
// lands on the year-10000 cell, while days inside the year keep theirs.
func TestCalendarGridWithholdsProjectionsPastYear9999(t *testing.T) {
	t.Parallel()
	monthStart := mustParseDashboardDay(t, "9999-12-01")

	for _, tc := range []struct {
		name      string
		lastStart string
		today     string
		// windowFrom is the first day that must carry no window marker.
		windowFrom string
		// wantWindowDay is a window day before windowFrom the grid still paints.
		wantWindowDay string
		wantPredicted []string
	}{
		// Current window 9999-12-28..10000-01-02: the stats withhold it, and the
		// grid's fallback must not paint the 12-25..12-27 lead-in without it.
		{name: "current window past the year", lastStart: "9999-12-20", today: "9999-12-30", windowFrom: "9999-12-20"},
		// Chained cycle from 9999-12-23 ovulates on 10000-01-05; the current
		// window (ovulation 9999-12-08) stays.
		{name: "chained window past the year", lastStart: "9999-11-25", today: "9999-11-30", windowFrom: "9999-12-23", wantWindowDay: "9999-12-08", wantPredicted: []string{"9999-12-23", "9999-12-27"}},
		// Chained band from 9999-12-30 runs into 10000-01-01.
		{name: "band crossing the year", lastStart: "9999-12-02", today: "9999-12-10", windowFrom: "9999-12-30", wantWindowDay: "9999-12-15", wantPredicted: []string{"9999-12-30", "9999-12-31"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			owner := lateYear9999Owner()
			logs := lateYear9999Periods(t, tc.lastStart)
			now := mustParseDashboardDay(t, tc.today)
			stats := BuildCycleStatsFromLogs(owner, logs, now, time.UTC)
			days := BuildCalendarDayStates(owner, monthStart, logs, stats, now, time.UTC)

			if last := days[len(days)-1].DateString; last != "10000-01-01" {
				t.Fatalf("precondition: grid ends on %s, want 10000-01-01", last)
			}
			byKey := make(map[string]CalendarDayState, len(days))
			for _, day := range days {
				byKey[day.DateString] = day
				if day.Date.Year() > 9999 {
					if flags := calendarProjectedFlags(day); len(flags) != 0 {
						t.Fatalf("%s carries %v, want no projection after 9999-12-31", day.DateString, flags)
					}
					continue
				}
				if day.DateString >= tc.windowFrom && calendarWindowFlags(day) {
					t.Fatalf("%s carries %v, want the window past 9999-12-31 withheld as a whole", day.DateString, calendarProjectedFlags(day))
				}
			}
			if tc.wantWindowDay != "" && !byKey[tc.wantWindowDay].IsOvulation {
				t.Fatalf("control: %s carries %v, want the in-year ovulation painted", tc.wantWindowDay, calendarProjectedFlags(byKey[tc.wantWindowDay]))
			}
			for _, key := range tc.wantPredicted {
				if !byKey[key].IsPredicted {
					t.Fatalf("control: %s carries %v, want the in-year predicted period painted", key, calendarProjectedFlags(byKey[key]))
				}
			}
		})
	}
}
