package services

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Regression locks for the mixed-midnight comparison class (issue #48 family).
//
// A calendar day built in the request/owner zone (DateAtLocation, CalendarDay
// with a non-UTC location) and a calendar day built at UTC midnight (dateOnly,
// every PredictCycleWindow output) name the SAME date but are DIFFERENT
// instants: local midnight in a UTC-minus zone lands hours AFTER UTC midnight,
// and in a UTC-plus zone hours before it. Comparing the pair with raw
// Before/After/Equal therefore misreads the same calendar day, every day, in
// every non-UTC zone — no DST transition is involved.
//
// Each test below drives the zone whose sign makes its site break (west of UTC
// for the "ovulation already passed" shape, east for the "log is in the future"
// shape, both for the ribbon) and carries a UTC control that must stay
// unchanged. The sanctioned instrument is CalendarDaysBetween, which re-anchors
// both operands to UTC midnight of their own calendar day.

// calendarDayComparisonZone loads a named zone for these regressions. A missing
// zone is a hard failure, not a skip: a skipped guard proves nothing about the
// defect it was written for.
func calendarDayComparisonZone(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return location
}

// calendarDayComparisonToday builds "today" the way every request path does:
// DateAtLocation over a real instant, which yields local midnight.
func calendarDayComparisonToday(t *testing.T, day string, location *time.Location) time.Time {
	t.Helper()
	// Midday UTC sits inside the same calendar day in every zone used here.
	return DateAtLocation(mustParseDashboardDay(t, day).Add(12*time.Hour), location)
}

// TestDashboardUpcomingPredictionsKeepsTodaysOvulation locks dashboard_cycle.go:
// window.OvulationDate (UTC midnight) against today (location midnight). West of
// UTC today's ovulation read as already past, ShiftCycleStartToFutureOvulation
// fired, and every owner-facing surface rolled the anchor a FULL CYCLE forward.
func TestDashboardUpcomingPredictionsKeepsTodaysOvulation(t *testing.T) {
	t.Parallel()

	// Cycle start 2026-03-02, length 28, luteal 14 → ovulation on cycle day 14,
	// i.e. exactly 2026-03-15, the day the render sits on.
	const cycleStart = "2026-03-02"
	const ovulationDay = "2026-03-15"

	for _, testCase := range []struct{ name, zone string }{
		{name: "west of UTC", zone: "America/Lima"},
		{name: "UTC control", zone: "UTC"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location := calendarDayComparisonZone(t, testCase.zone)
			today := calendarDayComparisonToday(t, ovulationDay, location)
			stats := CycleStats{
				LastPeriodStart: mustParseDashboardDay(t, cycleStart),
				LutealPhase:     14,
			}

			prediction := DashboardUpcomingPredictions(stats, nil, today, 28)
			if got := CalendarDayKey(prediction.OvulationDate); got != ovulationDay {
				t.Fatalf("ovulation on the day itself = %s, want %s (the anchor must not roll a cycle forward)", got, ovulationDay)
			}
		})
	}
}

// TestShiftCycleStartToFutureOvulationKeepsTodaysAnchor locks cycle_baseline.go:
// the guard inside the shift itself. Its own lag arithmetic already counts
// calendar days (CalendarDaysBetween); only the guard compared instants, so the
// shift ran with a lag of zero and added a whole cycle.
func TestShiftCycleStartToFutureOvulationKeepsTodaysAnchor(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ name, zone string }{
		{name: "west of UTC", zone: "America/New_York"},
		{name: "UTC control", zone: "UTC"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location := calendarDayComparisonZone(t, testCase.zone)
			today := calendarDayComparisonToday(t, "2026-03-15", location)
			cycleStart := CalendarDay(mustParseDashboardDay(t, "2026-03-02"), location)
			ovulationDate := dateOnly(mustParseDashboardDay(t, "2026-03-15"))

			shifted := ShiftCycleStartToFutureOvulation(cycleStart, ovulationDate, 28, today)
			if got := CalendarDayKey(shifted); got != "2026-03-02" {
				t.Fatalf("anchor after an ovulation that is today = %s, want 2026-03-02 (no shift)", got)
			}

			// Positive anchor: an ovulation genuinely in the past still shifts.
			pastOvulation := dateOnly(mustParseDashboardDay(t, "2026-03-14"))
			if got := CalendarDayKey(ShiftCycleStartToFutureOvulation(cycleStart, pastOvulation, 28, today)); got != "2026-03-30" {
				t.Fatalf("anchor after a past ovulation = %s, want 2026-03-30", got)
			}
		})
	}
}

// TestFilterLogsNotAfterKeepsTodaysLogEastOfUTC locks cycles.go: dateOnly(log.Date)
// (UTC midnight) against a cutoff its callers build at location midnight
// (calendar_days.go, cycle_start_policy.go, stats_cycle_insights.go). East of UTC
// today's own entry was dropped, so a same-day BBT shift was never detected and
// the calendar kept painting a tentative ovulation.
func TestFilterLogsNotAfterKeepsTodaysLogEastOfUTC(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ name, zone string }{
		{name: "east of UTC", zone: "Europe/Moscow"},
		{name: "far east of UTC", zone: "Asia/Tokyo"},
		{name: "UTC control", zone: "UTC"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location := calendarDayComparisonZone(t, testCase.zone)
			cutoff := calendarDayComparisonToday(t, "2026-03-15", location)
			logs := []models.DailyLog{
				{Date: mustParseDashboardDay(t, "2026-03-14")},
				{Date: mustParseDashboardDay(t, "2026-03-15")},
				{Date: mustParseDashboardDay(t, "2026-03-16")},
			}

			filtered := filterLogsNotAfter(logs, cutoff)
			kept := make([]string, 0, len(filtered))
			for _, entry := range filtered {
				kept = append(kept, CalendarDayKey(entry.Date))
			}
			if strings.Join(kept, ",") != "2026-03-14,2026-03-15" {
				t.Fatalf("logs kept up to and including today = %v, want [2026-03-14 2026-03-15]", kept)
			}
		})
	}
}

// TestCalendarFeedKeepsTodaysOvulationEventWestOfUTC locks calendar_feed_ics.go:
// west of UTC the ovulation event vanished from the .ics on the ovulation day
// itself, while the period event one line above it — both operands already
// location midnight — stayed. One function, two shapes.
func TestCalendarFeedKeepsTodaysOvulationEventWestOfUTC(t *testing.T) {
	t.Parallel()

	// dayBoundaryFeedLogs puts the current cycle's projected ovulation exactly on
	// 2026-03-10, the calendar day the feed is rendered on.
	const ovulationEvent = "DTSTART;VALUE=DATE:20260310"

	for _, testCase := range []struct{ name, zone string }{
		{name: "west of UTC", zone: "America/Lima"},
		{name: "UTC control", zone: "UTC"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location := calendarDayComparisonZone(t, testCase.zone)
			body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
				User:       predictableFeedUser(t, "2026-02-25"),
				Logs:       dayBoundaryFeedLogs(t),
				Now:        mustParseDashboardDay(t, "2026-03-10").Add(12 * time.Hour),
				Location:   location,
				Disclaimer: "Predictions are estimates, not medical advice or a method of contraception.",
			}))

			if !strings.Contains(body, ovulationEvent) {
				t.Fatalf("the ovulation event on the ovulation day must stay in the feed, got:\n%s", body)
			}
		})
	}
}

// TestOvulationCycleAnchorKeepsTodaysCycleWestOfUTC locks webhook_reminder.go:
// the same shift guard feeding the OUTBOUND reminder's anchor, which must stay
// in lockstep with the date the dashboard shows.
func TestOvulationCycleAnchorKeepsTodaysCycleWestOfUTC(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ name, zone string }{
		{name: "west of UTC", zone: "America/Lima"},
		{name: "UTC control", zone: "UTC"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location := calendarDayComparisonZone(t, testCase.zone)
			today := calendarDayComparisonToday(t, "2026-03-15", location)
			stats := CycleStats{
				LastPeriodStart: mustParseDashboardDay(t, "2026-03-02"),
				LutealPhase:     14,
			}

			if got := CalendarDayKey(ovulationCycleAnchor(stats, today, 28)); got != "2026-03-02" {
				t.Fatalf("reminder anchor on the ovulation day = %s, want 2026-03-02", got)
			}
		})
	}
}

// TestDashboardOvulationInPastIsCalendarBound locks the non-range branch of
// dashboard_cycle.go's dashboardOvulationInPast: display.ovulationDate is the
// PredictCycleWindow output (UTC midnight) while today is a location midnight.
// West of UTC the ovulation day itself read as already past, so the dashboard
// named the ovulation date and printed the amber "date is already in the past"
// warning beside it.
//
// The range branch one line above is the control that must stay untouched: both
// of its operands come from DashboardOvulationRange, built with CalendarDay in
// the request location, so they already share a shape.
func TestDashboardOvulationInPastIsCalendarBound(t *testing.T) {
	t.Parallel()

	const renderDay = "2026-02-16"

	for _, testCase := range []struct{ name, zone string }{
		{name: "west of UTC", zone: "America/New_York"},
		{name: "east of UTC", zone: "Asia/Tokyo"},
		{name: "UTC control", zone: "UTC"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location := calendarDayComparisonZone(t, testCase.zone)
			today := calendarDayComparisonToday(t, renderDay, location)

			todaysOvulation := dashboardPredictionDisplay{
				ovulationDate: dateOnly(mustParseDashboardDay(t, renderDay)),
			}
			if dashboardOvulationInPast(todaysOvulation, today) {
				t.Fatalf("an ovulation predicted for today must not read as already past")
			}

			// Positive anchor: an ovulation genuinely on an earlier calendar day
			// still reports true, so the guard cannot be satisfied by disabling
			// the state altogether.
			pastOvulation := dashboardPredictionDisplay{
				ovulationDate: dateOnly(mustParseDashboardDay(t, "2026-02-15")),
			}
			if !dashboardOvulationInPast(pastOvulation, today) {
				t.Fatalf("an ovulation on an earlier calendar day must still read as past")
			}

			// The range branch, unchanged: same-shape operands on both sides.
			todaysRangeEnd := dashboardPredictionDisplay{
				ovulationUseRange:   true,
				ovulationRangeEnd:   CalendarDay(mustParseDashboardDay(t, renderDay), location),
				ovulationRangeStart: CalendarDay(mustParseDashboardDay(t, "2026-02-12"), location),
			}
			if dashboardOvulationInPast(todaysRangeEnd, today) {
				t.Fatalf("an ovulation range ending today must not read as already past")
			}
			pastRangeEnd := todaysRangeEnd
			pastRangeEnd.ovulationRangeEnd = CalendarDay(mustParseDashboardDay(t, "2026-02-15"), location)
			if !dashboardOvulationInPast(pastRangeEnd, today) {
				t.Fatalf("an ovulation range that ended yesterday must still read as past")
			}
		})
	}
}

// TestBuildDashboardCycleContextKeepsTodaysOvulationOutOfThePastWarning drives
// the same site through the surface that renders it: an ordinary account, no
// irregular mode, no DST date, whose projected ovulation falls on the day the
// dashboard is rendered. The context must name the date AND leave
// OvulationInPast false — naming the date while flagging it as past is the pair
// the owner saw.
func TestBuildDashboardCycleContextKeepsTodaysOvulationOutOfThePastWarning(t *testing.T) {
	t.Parallel()

	// Anchor 2026-02-03 with a 28-day cycle and a 14-day luteal phase puts
	// ovulation on cycle day 14, i.e. exactly 2026-02-16.
	const cycleStart = "2026-02-03"
	const ovulationDay = "2026-02-16"

	for _, testCase := range []struct{ name, zone string }{
		{name: "west of UTC", zone: "America/New_York"},
		{name: "east of UTC", zone: "Asia/Tokyo"},
		{name: "UTC control", zone: "UTC"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location := calendarDayComparisonZone(t, testCase.zone)
			today := calendarDayComparisonToday(t, ovulationDay, location)
			user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5}
			stats := CycleStats{
				LastPeriodStart:     CalendarDay(mustParseDashboardDay(t, cycleStart), location),
				MedianCycleLength:   28,
				AverageCycleLength:  28,
				AveragePeriodLength: 5,
				LutealPhase:         14,
				CurrentCycleDay:     14,
			}

			context := BuildDashboardCycleContext(user, stats, today, location)
			if got := CalendarDayKey(context.DisplayOvulationDate); got != ovulationDay {
				t.Fatalf("displayed ovulation date = %s, want %s", got, ovulationDay)
			}
			if context.OvulationInPast {
				t.Fatalf("the dashboard must not flag today's ovulation date as already in the past")
			}
			if context.NextPeriodInPast {
				t.Fatalf("the next-period warning is a separate predicate and must stay false here")
			}
		})
	}
}

// TestStatsCycleRibbonFertileWindowIsCalendarBound locks stats_cycle_ribbon.go,
// the one site that breaks in BOTH directions: the row's dates are location
// midnights while the window bounds are UTC midnights, so west of UTC the last
// fertile day fell outside the window and east of UTC the first one did — and
// date.Equal(window.OvulationDate) was never true outside UTC, so IsFertilePeak
// never rendered while the neighbouring phase axis, which already routes through
// CalendarDaysBetween, coloured that same cell "ovulation".
func TestStatsCycleRibbonFertileWindowIsCalendarBound(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ name, zone string }{
		{name: "west of UTC", zone: "America/Lima"},
		{name: "east of UTC", zone: "Asia/Tokyo"},
		{name: "UTC control", zone: "UTC"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location := calendarDayComparisonZone(t, testCase.zone)
			start := CalendarDay(mustParseDashboardDay(t, "2026-03-02"), location)
			span := completedCycleSpan{
				Start:        start,
				NextStart:    CalendarDay(mustParseDashboardDay(t, "2026-03-30"), location),
				CycleLength:  28,
				PeriodLength: 5,
			}

			row := statsCycleRibbonRow(span, 28, 14, true, map[string]bool{})

			// Cycle length 28, luteal 14 → ovulation on day 14, fertile window
			// days 9..14 (ovulation minus five days through ovulation itself).
			for _, day := range []int{9, 14} {
				if !row.Days[day-1].IsFertile {
					t.Fatalf("day %d must be inside the fertile window", day)
				}
			}
			if row.Days[7].IsFertile {
				t.Fatalf("day 8 must sit outside the fertile window")
			}
			if !row.Days[13].IsFertilePeak {
				t.Fatalf("day 14 must be the fertile peak")
			}
			// The two axes of the same cell must agree.
			if row.Days[13].Phase != "ovulation" {
				t.Fatalf("day 14 phase = %q, want \"ovulation\"", row.Days[13].Phase)
			}
		})
	}
}
