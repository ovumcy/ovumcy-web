package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-381: the period-span and fertile-window opt-ins of the .ics feed. Each is
// off by default (the byte-identical body is pinned by
// TestCalendarFeedNeutralDefaultIsByteIdenticalWithoutTheOptIn), and each sends
// only what the calendar grid shades. Cited by SECURITY.md's Calendar Feed rows.

const spanTestNotice = "Pregnancy is possible on any day, including outside these dates."

// spanFeedLogs records cycle starts 28 days apart ending 2026-03-02, each with
// five logged period days, so the observed period length is five.
func spanFeedLogs(t *testing.T, starts ...string) []models.DailyLog {
	t.Helper()
	logs := make([]models.DailyLog, 0, len(starts)*5)
	for _, start := range starts {
		day := mustParseDashboardDay(t, start)
		for offset := range 5 {
			logs = append(logs, models.DailyLog{Date: day.AddDate(0, 0, offset), IsPeriod: true, CycleStart: offset == 0})
		}
	}
	return logs
}

var spanFeedThreeCycles = []string{"2025-12-08", "2026-01-05", "2026-02-02", "2026-03-02"}

func spanFeedInput(t *testing.T, today string, periodSpan, fertileWindow bool, starts ...string) CalendarFeedICSInput {
	t.Helper()
	user := predictableFeedUser(t, starts[len(starts)-1])
	user.CalendarFeedPeriodSpan = periodSpan
	user.CalendarFeedFertileWindow = fertileWindow
	return CalendarFeedICSInput{
		User:                user,
		Logs:                spanFeedLogs(t, starts...),
		Now:                 mustParseDashboardDay(t, today),
		Location:            time.UTC,
		Disclaimer:          "Predictions are estimates, not medical advice or a method of contraception.",
		SecretKey:           []byte(calendarFeedTestSecretKey),
		FertileWindowNotice: spanTestNotice,
	}
}

func feedEventsOfKind(events []calendarFeedEvent, kind string) []calendarFeedEvent {
	var matched []calendarFeedEvent
	for _, event := range events {
		if event.kind == kind {
			matched = append(matched, event)
		}
	}
	return matched
}

func feedEventDay(event calendarFeedEvent) string { return event.date.Format("2006-01-02") }
func feedEventEnd(event calendarFeedEvent) string { return event.end.Format("2006-01-02") }

func TestCalendarFeedPeriodSpanReplacesTheSingleStartDay(t *testing.T) {
	input := spanFeedInput(t, "2026-03-20", true, false, spanFeedThreeCycles...)
	events := calendarFeedEvents(input)

	if single := feedEventsOfKind(events, calendarFeedKindPeriod); len(single) != 0 {
		t.Fatalf("a span must replace the single start day, got single days %v", single)
	}
	spans := feedEventsOfKind(events, calendarFeedKindPeriodSpan)
	want := [][2]string{{"2026-03-30", "2026-04-03"}, {"2026-04-27", "2026-05-01"}, {"2026-05-25", "2026-05-29"}}
	if len(spans) != len(want) {
		t.Fatalf("expected %d spans, got %v", len(want), spans)
	}
	for index, span := range spans {
		if feedEventDay(span) != want[index][0] || feedEventEnd(span) != want[index][1] {
			t.Fatalf("span %d = %s..%s, want %s..%s", index, feedEventDay(span), feedEventEnd(span), want[index][0], want[index][1])
		}
		if feedEventDay(span) == "2026-03-02" {
			t.Fatal("the running cycle's own period is a recorded start and must never be sent")
		}
	}

	body := string(buildTestFeedICS(input))
	if !strings.Contains(body, "DTSTART;VALUE=DATE:20260330\r\nDTEND;VALUE=DATE:20260404\r\n") {
		t.Fatalf("the first span must end on the exclusive day after its fifth day:\n%s", body)
	}
	if !strings.Contains(body, "UID:"+feedTestUID(t, input.User.ID, calendarFeedKindPeriodSpan, "20260330")) {
		t.Fatal("the span does not carry the period-span UID of its start")
	}
}

func TestCalendarFeedPeriodSpanNeedsAnObservedPeriodLength(t *testing.T) {
	events := calendarFeedEvents(spanFeedInput(t, "2026-03-05", true, false, "2026-03-02"))
	if spans := feedEventsOfKind(events, calendarFeedKindPeriodSpan); len(spans) != 0 {
		t.Fatalf("without a completed cycle the length is configuration, so no span may go out: %v", spans)
	}
	if single := feedEventsOfKind(events, calendarFeedKindPeriod); len(single) == 0 {
		t.Fatal("without a completed cycle the single start day must still go out, as before the opt-in")
	}
}

func TestCalendarFeedLateSpanKeepsItsStartUntilItEnds(t *testing.T) {
	late := calendarFeedEvents(spanFeedInput(t, "2026-03-31", true, false, spanFeedThreeCycles...))
	spans := feedEventsOfKind(late, calendarFeedKindPeriodSpan)
	if len(spans) != 1 || feedEventDay(spans[0]) != "2026-03-30" {
		t.Fatalf("a late period keeps its own start, and the chain after a due start is suppressed: %v", spans)
	}
	ended := calendarFeedEvents(spanFeedInput(t, "2026-04-04", true, false, spanFeedThreeCycles...))
	if spans := feedEventsOfKind(ended, calendarFeedKindPeriodSpan); len(spans) != 0 {
		t.Fatalf("a span whose last day is behind today must not be sent: %v", spans)
	}
}

func TestCalendarFeedSpanUIDSurvivesALengthChange(t *testing.T) {
	five := spanFeedInput(t, "2026-03-20", true, false, spanFeedThreeCycles...)
	four := spanFeedInput(t, "2026-03-20", true, false, spanFeedThreeCycles...)
	// spanFeedLogs writes five days per cycle in order: drop each fifth.
	var trimmed []models.DailyLog
	for index, log := range four.Logs {
		if index%5 != 4 {
			trimmed = append(trimmed, log)
		}
	}
	four.Logs = trimmed
	first := func(input CalendarFeedICSInput) calendarFeedEvent {
		spans := feedEventsOfKind(calendarFeedEvents(input), calendarFeedKindPeriodSpan)
		if len(spans) == 0 {
			t.Fatal("no span")
		}
		return spans[0]
	}
	a, b := first(five), first(four)
	if feedEventDay(a) != feedEventDay(b) {
		t.Fatalf("a changed length must not move the start: %s vs %s", feedEventDay(a), feedEventDay(b))
	}
	if feedEventEnd(a) == feedEventEnd(b) {
		t.Fatal("the fixture did not change the observed period length, so this test measures nothing")
	}
}

func TestCalendarFeedSpansFollowEverySuppression(t *testing.T) {
	input := spanFeedInput(t, "2026-03-20", true, true, spanFeedThreeCycles...)
	input.User.UnpredictableCycle = true
	if events := calendarFeedEvents(input); len(events) != 0 {
		t.Fatalf("unpredictable-cycle mode must send no span: %v", events)
	}
	paused := spanFeedInput(t, "2026-03-20", true, true, spanFeedThreeCycles...)
	paused.Logs = append(paused.Logs, models.DailyLog{Date: mustParseDashboardDay(t, "2026-03-10"), PregnancyTest: models.PregnancyTestPositive})
	if events := calendarFeedEvents(paused); len(events) != 0 {
		t.Fatalf("a pregnancy pause must send no span: %v", events)
	}
	overdue := spanFeedInput(t, "2026-04-20", true, true, spanFeedThreeCycles...)
	if events := calendarFeedEvents(overdue); len(events) != 0 {
		t.Fatalf("an overdue cycle must send no span: %v", events)
	}
	blank := spanFeedInput(t, "2026-03-20", true, true, spanFeedThreeCycles...)
	blank.Disclaimer = " "
	if body := string(buildTestFeedICS(blank)); strings.Contains(body, "BEGIN:VEVENT") {
		t.Fatal("a blank disclaimer must still render zero events")
	}
}

func TestCalendarFeedFertileWindowKeepsTheGridsFloor(t *testing.T) {
	for name, starts := range map[string][]string{
		"no completed cycle":   {"2026-03-02"},
		"two completed cycles": {"2026-01-05", "2026-02-02", "2026-03-02"},
	} {
		t.Run(name, func(t *testing.T) {
			events := calendarFeedEvents(spanFeedInput(t, "2026-03-05", false, true, starts...))
			if windows := feedEventsOfKind(events, calendarFeedKindFertileWindow); len(windows) != 0 {
				t.Fatalf("below the completed-cycle floor no fertile window may go out: %v", windows)
			}
		})
	}
	events := calendarFeedEvents(spanFeedInput(t, "2026-03-05", false, true, spanFeedThreeCycles...))
	if windows := feedEventsOfKind(events, calendarFeedKindFertileWindow); len(windows) != 2 {
		t.Fatalf("three completed cycles send the running window and N+1 only, got %v", windows)
	}
}

func TestCalendarFeedFertileWindowStopsAtNPlusOneAndDropsWhatIsBehindToday(t *testing.T) {
	events := calendarFeedEvents(spanFeedInput(t, "2026-03-20", false, true, spanFeedThreeCycles...))
	windows := feedEventsOfKind(events, calendarFeedKindFertileWindow)
	if len(windows) != 1 {
		t.Fatalf("with the running window behind today only N+1 remains, got %v", windows)
	}
	today := mustParseDashboardDay(t, "2026-03-20")
	if CalendarDaysBetween(today, windows[0].end) < 0 || CalendarDaysBetween(mustParseDashboardDay(t, "2026-03-30"), windows[0].date) < 0 {
		t.Fatalf("the window sent must be the next cycle's, not one behind today: %v", windows)
	}
}

func TestCalendarFeedFertileWindowCarriesTheNoticeOrNothing(t *testing.T) {
	input := spanFeedInput(t, "2026-03-20", false, true, spanFeedThreeCycles...)
	body := string(buildTestFeedICS(input))
	if !strings.Contains(strings.ReplaceAll(body, "\r\n ", ""), escapeICSText(input.Disclaimer+"\n"+spanTestNotice)) {
		t.Fatalf("a fertile-window event must carry the disclaimer and then the notice:\n%s", body)
	}
	for _, event := range strings.Split(body, "BEGIN:VEVENT")[1:] {
		if strings.Count(event, "\r\nDESCRIPTION:") != 1 {
			t.Fatalf("every event must carry exactly one DESCRIPTION:\n%s", event)
		}
	}
	input.FertileWindowNotice = "  "
	if windows := feedEventsOfKind(calendarFeedEvents(input), calendarFeedKindFertileWindow); len(windows) != 0 {
		t.Fatalf("a blank notice must withhold every fertile-window event: %v", windows)
	}
}

func TestCalendarFeedTitlesRequireTheFertileTitleOnlyUnderItsOptIn(t *testing.T) {
	pair := CalendarFeedTitles{Period: "Period (estimate)", Ovulation: "Ovulation (estimate)"}
	if !pair.complete(false) || pair.complete(true) {
		t.Fatal("the fertile title is required exactly when the fertile opt-in is on")
	}
	all := pair
	all.FertileWindow = "Fertile window (estimate)"
	if !all.complete(true) {
		t.Fatal("three titles are complete under the fertile opt-in")
	}
	if calendarFeedSummary(calendarFeedKindFertileWindow, &all) != all.FertileWindow || calendarFeedSummary(calendarFeedKindPeriodSpan, &all) != all.Period {
		t.Fatal("a span takes its type's title")
	}
}

// A cycle bounded only by the onboarding start completes without a period day,
// and while the next period is still running the configured length stands in
// for the average (BuildCycleStatsFromLogs). A completed cycle is therefore not
// proof of an observed length: the floor also needs the length read off logged
// period days.
func TestPeriodLengthObservedNeedsALoggedLengthNotJustACompletedCycle(t *testing.T) {
	for name, tc := range map[string]struct {
		stats CycleStats
		want  bool
	}{
		"no completed cycle":                 {CycleStats{PeriodLengthObserved: true}, false},
		"completed cycle, configured length": {CycleStats{CompletedCycleCount: 1, AveragePeriodLength: 6}, false},
		"completed cycle, logged length":     {CycleStats{CompletedCycleCount: 1, AveragePeriodLength: 5, PeriodLengthObserved: true}, true},
	} {
		if got := PeriodLengthObserved(tc.stats); got != tc.want {
			t.Errorf("%s: PeriodLengthObserved = %v, want %v", name, got, tc.want)
		}
	}
	logged := spanFeedInput(t, "2026-03-05", true, false, spanFeedThreeCycles...)
	if !BuildCycleStatsFromLogs(logged.User, logged.Logs, logged.Now, time.UTC).PeriodLengthObserved {
		t.Fatal("five logged period days per cycle must mark the length observed")
	}

	// The period fills write the configured length as auto-filled days behind a
	// ticked start: that length is configuration, so no span goes out.
	// The configured length is the owner's (ownerRunningPeriodRule), so these
	// fixtures are owners.
	filled := spanFeedInput(t, "2026-03-05", true, false, spanFeedThreeCycles...)
	filled.User.Role = models.RoleOwner
	autoFilled := true
	for i := range filled.Logs {
		if !filled.Logs[i].CycleStart {
			filled.Logs[i].AutoFilled = &autoFilled
		}
	}
	if BuildCycleStatsFromLogs(filled.User, filled.Logs, filled.Now, time.UTC).PeriodLengthObserved {
		t.Fatal("auto-filled days up to the configured length must not mark the length observed")
	}
	events := calendarFeedEvents(filled)
	if spans := feedEventsOfKind(events, calendarFeedKindPeriodSpan); len(spans) != 0 {
		t.Fatalf("a span drawn from the fill's configured length went out: %v", spans)
	}
	if len(feedEventsOfKind(events, calendarFeedKindPeriod)) == 0 {
		t.Fatal("the single start day must still go out")
	}

	// A fill the owner trimmed to four days is the owner's length.
	trimmed := spanFeedInput(t, "2026-03-05", true, false, spanFeedThreeCycles...)
	trimmed.User.Role = models.RoleOwner
	kept := trimmed.Logs[:0]
	for _, log := range trimmed.Logs {
		if !log.CycleStart {
			log.AutoFilled = &autoFilled
		}
		if log.Date.Equal(mustParseDashboardDay(t, "2026-02-06")) {
			continue
		}
		kept = append(kept, log)
	}
	trimmed.Logs = kept
	if !BuildCycleStatsFromLogs(trimmed.User, trimmed.Logs, trimmed.Now, time.UTC).PeriodLengthObserved {
		t.Fatal("a fill trimmed below the configured length must mark the length observed")
	}
}

// Where the dashboard names a next-period start window, the feed sends that
// window and the cycle it covers gets no span: a solid span drawn from the
// median would show the window's edges as more precise than they are. The
// cycles chained after it still go out as spans.
func TestCalendarFeedPeriodSpanYieldsToTheStartWindow(t *testing.T) {
	base := mustParseDashboardDay(t, "2026-01-01")
	var logs []models.DailyLog
	for _, offset := range []int{0, 28, 56, 84, 112, 172} {
		for day := range 5 {
			logs = append(logs, models.DailyLog{Date: base.AddDate(0, 0, offset+day), IsPeriod: true, CycleStart: day == 0})
		}
	}
	user := regularWebhookUser()
	user.ID = 7
	user.CalendarFeedPeriodSpan = true
	events := calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: base.AddDate(0, 0, 190), Location: time.UTC})

	windows := feedEventsOfKind(events, calendarFeedKindPeriodWindow)
	if len(windows) != 1 {
		t.Fatalf("scenario setup: the spread should send one start window, got %v", windows)
	}
	spans := feedEventsOfKind(events, calendarFeedKindPeriodSpan)
	if len(spans) == 0 {
		t.Fatal("the cycles chained after the window must still go out as spans")
	}
	for _, span := range spans {
		if calendarDayWithin(span.date, windows[0].date, windows[0].end) {
			t.Fatalf("span %s..%s starts inside the start window %s..%s", feedEventDay(span), feedEventEnd(span), feedEventDay(windows[0]), feedEventEnd(windows[0]))
		}
	}
	if singles := feedEventsOfKind(events, calendarFeedKindPeriod); len(singles) != 0 {
		t.Fatalf("under the span opt-in no single start day may go out: %v", singles)
	}
}

// A fertile window is still sent on its own last day in a UTC-minus zone, where
// that local evening is already the next day in UTC (issue #48 class).
func TestCalendarFeedFertileWindowSurvivesItsLastEveningWestOfUTC(t *testing.T) {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	early := spanFeedInput(t, "2026-03-05", false, true, spanFeedThreeCycles...)
	early.Location = location
	early.Now = time.Date(2026, time.March, 5, 12, 0, 0, 0, location)
	windows := feedEventsOfKind(calendarFeedEvents(early), calendarFeedKindFertileWindow)
	if len(windows) == 0 {
		t.Fatal("scenario setup: the fixture sends no fertile window")
	}
	running := windows[0]

	lastEvening := early
	lastEvening.Now = time.Date(running.end.Year(), running.end.Month(), running.end.Day(), 20, 0, 0, 0, location)
	for _, window := range feedEventsOfKind(calendarFeedEvents(lastEvening), calendarFeedKindFertileWindow) {
		if feedEventDay(window) == feedEventDay(running) {
			return
		}
	}
	t.Fatalf("the window %s..%s left the feed on its own last local evening", feedEventDay(running), feedEventEnd(running))
}

// ResolveFeed resolves the fertile-window notice in the owner's language only
// for a link that opted in, and the served body carries it.
func TestResolveFeedCarriesTheFertileWindowNoticeOfAnOptedInLink(t *testing.T) {
	user, token := armedFeedUser(t, 42, "2026-03-02")
	user.CalendarFeedFertileWindow = true
	copyProvider := &recordingFeedCopy{titles: map[string]string{calendarFeedFertileWindowNoticeKey: spanTestNotice}}
	svc := NewCalendarFeedService(
		&stubFeedUserStore{selector: user.CalendarFeedSelector, user: user},
		&stubFeedDayReader{logs: predictableFeedLogs(t)},
		copyProvider,
		[]byte(calendarFeedTestSecretKey),
	)
	body, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err != nil || !ok {
		t.Fatalf("ResolveFeed: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(strings.ReplaceAll(string(body), "\r\n ", ""), escapeICSText("\n"+spanTestNotice)) {
		t.Fatalf("the served fertile-window events carry no notice:\n%s", body)
	}
}

// TestCalendarFeedSpansMatchTheGridDays pins the feed to the calendar grid: the
// days its period spans cover are the grid's projected period days for those
// cycles, and the days its fertile windows cover are the grid's fertile days
// for the cycles it sends.
func TestCalendarFeedSpansMatchTheGridDays(t *testing.T) {
	for _, today := range []string{"2026-03-05", "2026-03-20"} {
		t.Run(today, func(t *testing.T) {
			input := spanFeedInput(t, today, true, true, spanFeedThreeCycles...)
			events := calendarFeedEvents(input)
			stats, _ := PublishedStats(input.User, BuildCycleStatsFromLogs(input.User, input.Logs, input.Now, time.UTC), input.Logs, input.Now, time.UTC)
			grid := map[string]CalendarDayState{}
			for month := range 4 {
				for _, state := range BuildCalendarDayStates(input.User, time.Date(2026, time.Month(3+month), 1, 0, 0, 0, 0, time.UTC), input.Logs, stats, input.Now, time.UTC) {
					grid[state.Date.Format("2006-01-02")] = state
				}
			}
			for _, kind := range []string{calendarFeedKindPeriodSpan, calendarFeedKindFertileWindow} {
				sent := feedEventsOfKind(events, kind)
				if len(sent) == 0 {
					t.Fatalf("the fixture sends no %s, so the parity below measures nothing", kind)
				}
				for _, event := range sent {
					before := event.date.AddDate(0, 0, -1).Format("2006-01-02")
					after := event.end.AddDate(0, 0, 1).Format("2006-01-02")
					shaded := func(day string) bool {
						state := grid[day]
						if kind == calendarFeedKindPeriodSpan {
							return state.IsPredicted
						}
						return state.IsFertilityEdge || state.IsFertilityPeak
					}
					for day := event.date; !day.After(event.end); day = day.AddDate(0, 0, 1) {
						if !shaded(day.Format("2006-01-02")) {
							t.Fatalf("%s covers %s, which the grid does not shade", kind, day.Format("2006-01-02"))
						}
					}
					if shaded(before) || shaded(after) {
						t.Fatalf("%s %s..%s is narrower than the grid's band", kind, feedEventDay(event), feedEventEnd(event))
					}
				}
			}
		})
	}
}
