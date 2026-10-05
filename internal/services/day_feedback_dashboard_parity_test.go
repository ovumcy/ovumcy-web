package services

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The day-save message and the dashboard must call the same days fertile. The
// message used to derive its own stats with the default luteal phase of 14 and
// no thermal-shift substitution, while the dashboard goes through the owner's
// baseline, the confirmed-shift resolver and the publication gate — so with an
// inferred luteal phase of 10 the toast named days the dashboard did not shade
// and stayed quiet on days it did.
//
// Every case builds both surfaces over one history and one "now", then sweeps the
// days of a cycle: the message must be the fertile one on exactly the days the
// dashboard's published window covers.

func dayFeedbackParityUser(id uint) *models.User {
	return &models.User{ID: id, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14, TrackBBT: true}
}

// The parity must hold in every request zone, not only in UTC: the stats the
// feedback and the dashboard read put the window at the owner's local midnight,
// the day being saved arrives as a local midnight too, and in UTC alone the
// local and the UTC anchors coincide, so a comparison that mixed them stayed
// green there. The zones are one east of UTC (Pacific/Auckland, UTC+12/+13),
// one west (America/Los_Angeles, UTC-7/-8) and UTC itself.
func dayFeedbackParityZones(t *testing.T) map[string]*time.Location {
	t.Helper()

	zones := map[string]*time.Location{"UTC": time.UTC}
	for _, name := range []string{"Pacific/Auckland", "America/Los_Angeles"} {
		location, err := time.LoadLocation(name)
		if err != nil {
			t.Fatalf("LoadLocation(%s): %v", name, err)
		}
		zones[name] = location
	}
	return zones
}

// forEachParityZone runs the case once per zone, named after it.
func forEachParityZone(t *testing.T, run func(t *testing.T, location *time.Location)) {
	t.Helper()

	for name, location := range dayFeedbackParityZones(t) {
		t.Run(name, func(t *testing.T) { run(t, location) })
	}
}

// localNoon is the instant "now" is on the given calendar day for an owner in
// location: midday there, which is a different UTC calendar day in some zones.
func localNoon(day time.Time, location *time.Location) time.Time {
	year, month, dayOfMonth := day.Date()
	return time.Date(year, month, dayOfMonth, 12, 0, 0, 0, location)
}

// dayFeedbackFertileDays asks ResolveDayFeedback about every calendar day in
// [from, to] and returns the days it answers with the fertile message, in order.
// from, to and today name calendar days (their date components are read); each
// request is made the way the handler makes it, with the day at the owner's local
// midnight and "now" at local midday. A day the message resolves as self-care is
// skipped: the early period days carry their own message ahead of the window by
// design.
func dayFeedbackFertileDays(t *testing.T, user *models.User, logs []models.DailyLog, location *time.Location, today, from, to time.Time) []string {
	t.Helper()

	repository := newDayLogRepositoryStub()
	for _, entry := range logs {
		entry.UserID = user.ID
		repository.entries[CalendarDayKey(entry.Date)] = entry
	}
	service := NewDayService(repository, &dayUserRepositoryStub{})

	var fertile []string
	for day := dateOnly(from); !day.After(dateOnly(to)); day = day.AddDate(0, 0, 1) {
		state, err := service.ResolveDayFeedback(context.Background(), user, CalendarDay(day, location), localNoon(today, location), location)
		if err != nil {
			t.Fatalf("ResolveDayFeedback(%s) unexpected error: %v", CalendarDayKey(day), err)
		}
		if state.MessageKey == daySaveMessageFertile {
			fertile = append(fertile, CalendarDayKey(day))
		}
	}
	return fertile
}

// dashboardFertileDays reads the calendar days of [from, to] the dashboard's
// published window covers, under the same fertility gate that publishes it. The
// window is compared by calendar day, the way a reader of the page sees it.
func dashboardFertileDays(t *testing.T, user *models.User, logs []models.DailyLog, location *time.Location, today, from, to time.Time) []string {
	t.Helper()

	now := localNoon(today, location)
	dashboard := NewDashboardViewService(
		NewStatsService(nil, nil),
		&stubDashboardViewerProvider{logEntry: models.DailyLog{Date: now}},
		&stubDashboardDayStateProvider{logs: logs},
	)
	view, err := dashboard.BuildDashboardViewData(context.Background(), user, "en", now, location)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}

	var fertile []string
	if !view.ShowFertilityStatus || view.Stats.FertilityWindowStart.IsZero() {
		return fertile
	}
	for day := dateOnly(from); !day.After(dateOnly(to)); day = day.AddDate(0, 0, 1) {
		if CalendarDaysBetween(view.Stats.FertilityWindowStart, day) >= 0 && CalendarDaysBetween(day, view.Stats.FertilityWindowEnd) >= 0 {
			fertile = append(fertile, CalendarDayKey(day))
		}
	}
	return fertile
}

// onToday keeps only today's calendar-day key. The save message is the fertile
// one only for a save made on today itself, so it is held against the
// dashboard's window on that one day; a day behind the owner (a backfill) or
// ahead of them answers neutral whatever the window says.
func onToday(days []string, today time.Time) []string {
	kept := []string{}
	for _, key := range days {
		if key == CalendarDayKey(dateOnly(today)) {
			kept = append(kept, key)
		}
	}
	return kept
}

func dayFeedbackKeyOn(t *testing.T, user *models.User, logs []models.DailyLog, location *time.Location, today, day time.Time) string {
	t.Helper()

	repository := newDayLogRepositoryStub()
	for _, entry := range logs {
		entry.UserID = user.ID
		repository.entries[CalendarDayKey(entry.Date)] = entry
	}
	state, err := NewDayService(repository, &dayUserRepositoryStub{}).ResolveDayFeedback(context.Background(), user, CalendarDay(day, location), localNoon(today, location), location)
	if err != nil {
		t.Fatalf("ResolveDayFeedback(%s) unexpected error: %v", CalendarDayKey(day), err)
	}
	return state.MessageKey
}

// lutealTenLogs is four 28-day cycles from 2025-12-04 (three completed — the
// history the fertility half needs before the dashboard names a window) whose
// first three carry a thermal shift on cycle day 18, so the inferred luteal
// phase is 10 and the current cycle (from 2026-02-26) projects its window on
// cycle days 13-18, 2026-03-10..15. The default 14 would put it on days 9-14.
func lutealTenLogs(t *testing.T) []models.DailyLog {
	t.Helper()
	origin := time.Date(2025, time.December, 4, 0, 0, 0, 0, time.UTC)
	return mergeLogsByDay(lutealRoundTripLogs(t, origin, 28, []int{18, 18, 18}, lutealSignalBBT))
}

// mergeLogsByDay folds the fixture's separate period and temperature entries for
// one calendar day into the single row per day the store holds. The day-save
// path reads rows keyed by day, so a second entry for a day would replace the
// first and drop, for instance, the cycle start the temperature reading shares
// its date with.
func mergeLogsByDay(logs []models.DailyLog) []models.DailyLog {
	merged := make([]models.DailyLog, 0, len(logs))
	index := make(map[string]int, len(logs))
	for _, entry := range logs {
		key := CalendarDayKey(entry.Date)
		at, seen := index[key]
		if !seen {
			index[key] = len(merged)
			merged = append(merged, entry)
			continue
		}
		row := &merged[at]
		row.IsPeriod = row.IsPeriod || entry.IsPeriod
		row.CycleStart = row.CycleStart || entry.CycleStart
		if entry.Flow != "" {
			row.Flow = entry.Flow
		}
		if entry.BBT != nil {
			row.BBT = entry.BBT
		}
		if entry.CervicalMucus != "" {
			row.CervicalMucus = entry.CervicalMucus
		}
	}
	return merged
}

func TestDayFeedbackNamesTheSameFertileDaysAsTheDashboardForAnInferredLuteal(t *testing.T) {
	user := dayFeedbackParityUser(41)
	logs := lutealTenLogs(t)
	cycleStart := time.Date(2026, time.February, 26, 0, 0, 0, 0, time.UTC)
	cycleDay := func(day int) time.Time { return cycleStart.AddDate(0, 0, day-1) }

	// Fixture: the inference reaches this history, and the default window the old
	// message read is a different one — otherwise the case could pass on either.
	nowForStats := cycleDay(16)
	if got := BuildCycleStatsFromLogs(user, logs, nowForStats, time.UTC).LutealPhase; got != 10 {
		t.Fatalf("fixture: inferred luteal phase = %d, want 10", got)
	}
	defaultWindow := BuildCycleStats(logs, nowForStats)
	if first, last := CalendarDaysBetween(cycleStart, defaultWindow.FertilityWindowStart)+1, CalendarDaysBetween(cycleStart, defaultWindow.FertilityWindowEnd)+1; first != 9 || last != 14 {
		t.Fatalf("fixture: the default-luteal window is on cycle days %d-%d, want 9-14", first, last)
	}

	wantDays := []string{"2026-03-10", "2026-03-11", "2026-03-12", "2026-03-13", "2026-03-14", "2026-03-15"} // cycle days 13-18

	// The owner saves while looking at cycle day 16, inside the personal window
	// and past the default one, and at cycle day 10, the reverse.
	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		for _, today := range []time.Time{cycleDay(16), cycleDay(10)} {
			t.Run(CalendarDayKey(today), func(t *testing.T) {
				from, to := cycleDay(4), cycleDay(27)
				fromFeedback := dayFeedbackFertileDays(t, user, logs, location, today, from, to)
				fromDashboard := dashboardFertileDays(t, user, logs, location, today, from, to)
				if !slices.Equal(fromDashboard, wantDays) {
					t.Fatalf("dashboard window covers %v, want cycle days 13-18 %v", fromDashboard, wantDays)
				}
				if !slices.Equal(fromFeedback, onToday(fromDashboard, today)) {
					t.Fatalf("save message is fertile on %v, the dashboard window covers %v on today", fromFeedback, onToday(fromDashboard, today))
				}
				if !slices.Equal(fromFeedback, onToday(wantDays, today)) {
					t.Fatalf("fertile days = %v, want cycle days 13-18 on today only %v", fromFeedback, onToday(wantDays, today))
				}
				if got := dayFeedbackKeyOn(t, user, logs, location, today, cycleDay(10)); got != daySaveMessageNeutral {
					t.Fatalf("cycle day 10 message = %q, want the neutral one", got)
				}
				// Cycle day 16 is inside the window: fertile when it is today, neutral
				// when it is a day ahead (today = cycle day 10).
				wantDay16 := daySaveMessageNeutral
				if CalendarDayKey(today) == CalendarDayKey(cycleDay(16)) {
					wantDay16 = daySaveMessageFertile
				}
				if got := dayFeedbackKeyOn(t, user, logs, location, today, cycleDay(16)); got != wantDay16 {
					t.Fatalf("cycle day 16 message = %q, want %q", got, wantDay16)
				}
			})
		}
	})
}

// A thermal shift the owner's own temperatures confirm outranks the projection
// on the dashboard, and the message follows it: here the shift names cycle day 14
// while the inferred luteal phase projects day 18.
func TestDayFeedbackFollowsAConfirmedThermalShiftLikeTheDashboard(t *testing.T) {
	user := dayFeedbackParityUser(42)
	logs := lutealTenLogs(t)
	cycleStart := time.Date(2026, time.February, 26, 0, 0, 0, 0, time.UTC)
	cycleDay := func(day int) time.Time { return cycleStart.AddDate(0, 0, day-1) }

	for offset := range 6 {
		logs = append(logs, models.DailyLog{Date: cycleStart.AddDate(0, 0, offset), BBT: new(thermalShiftLowBBT)})
	}
	for offset := 14; offset <= 16; offset++ {
		logs = append(logs, models.DailyLog{Date: cycleStart.AddDate(0, 0, offset), BBT: new(thermalShiftHighBBT)})
	}
	logs = mergeLogsByDay(logs)
	today := cycleDay(17)

	// Fixture: the shift is confirmed on cycle day 14, three days short of the
	// projection's day 18.
	stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
	if stats.LutealPhase != 10 {
		t.Fatalf("fixture: inferred luteal phase = %d, want 10", stats.LutealPhase)
	}
	confirmedDay, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC)
	if !ok || CalendarDayKey(confirmedDay) != "2026-03-11" {
		t.Fatalf("fixture: confirmed ovulation = %s (ok=%t), want 2026-03-11", CalendarDayKey(confirmedDay), ok)
	}
	if got := CalendarDayKey(stats.OvulationDate); got != "2026-03-15" {
		t.Fatalf("fixture: projected ovulation = %s, want 2026-03-15", got)
	}

	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		from, to := cycleDay(4), cycleDay(27)
		fromFeedback := dayFeedbackFertileDays(t, user, logs, location, today, from, to)
		fromDashboard := dashboardFertileDays(t, user, logs, location, today, from, to)
		wantDays := []string{"2026-03-06", "2026-03-07", "2026-03-08", "2026-03-09", "2026-03-10", "2026-03-11"} // cycle days 9-14
		if !slices.Equal(fromDashboard, wantDays) {
			t.Fatalf("dashboard window covers %v, want the confirmed window %v", fromDashboard, wantDays)
		}
		if !slices.Equal(fromFeedback, onToday(fromDashboard, today)) {
			t.Fatalf("save message is fertile on %v, the dashboard window covers %v on today", fromFeedback, onToday(fromDashboard, today))
		}
		// A shift is only confirmed once its third warm day is logged, so by then the
		// whole confirmed window is behind the owner: every day of it is a backfill
		// and answers neutral, which leaves the sweep empty.
		if len(fromFeedback) != 0 {
			t.Fatalf("a backfilled day of the confirmed window got the fertile message on %v", fromFeedback)
		}
		// The projection's own days are past the confirmed ovulation: no fertile line.
		if got := dayFeedbackKeyOn(t, user, logs, location, today, cycleDay(16)); got != daySaveMessageNeutral {
			t.Fatalf("cycle day 16 message = %q, want the neutral one after the confirmed ovulation", got)
		}
	})
}

// The same history the webhook parity test uses: more than two years of it, so
// the dashboard's two-year window and the whole stored history disagree about
// whether the cycle is overdue. The message reads the dashboard's window.
//
// The fertile line belongs to a save made today, so each day of the sweep is
// saved on itself: the message and the dashboard are both asked as of that day.
// Holding one fixed "today" against past days would compare an empty list with
// an empty one, since a backfilled day answers neutral whatever the window says.
func TestDayFeedbackReadsTheDashboardsHistoryWindow(t *testing.T) {
	today := time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC)

	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		for _, testCase := range historyWindowCases() {
			t.Run(testCase.name, func(t *testing.T) {
				user := historyWindowUser()
				logs := historyWindowLogs(today, testCase.startsAgo)

				var fromFeedback, fromDashboard []string
				for day := today.AddDate(0, 0, -45); !day.After(today.AddDate(0, 0, 5)); day = day.AddDate(0, 0, 1) {
					// The first days of a period carry their own message ahead of the window
					// by design (dayFeedbackFertileDays skips them for the same reason).
					if dayFeedbackKeyOn(t, user, logs, location, day, day) == daySaveMessageSelfCare {
						continue
					}
					feedback := dayFeedbackFertileDays(t, user, logs, location, day, day, day)
					dashboard := dashboardFertileDays(t, user, logs, location, day, day, day)
					if !slices.Equal(feedback, dashboard) {
						t.Fatalf("saved on %s, the message is fertile on %v, the dashboard window covers %v", CalendarDayKey(day), feedback, dashboard)
					}
					if testCase.wantPaused && day.Equal(today) && len(feedback) != 0 {
						t.Fatalf("a paused history still gets the fertile message on %v", feedback)
					}
					fromFeedback = append(fromFeedback, feedback...)
					fromDashboard = append(fromDashboard, dashboard...)
				}
				// Control: a history the dashboard does project must reach the message,
				// so a message that went silent for everyone would not pass the
				// comparison above on two empty lists. The control is the steady
				// history: the old-short-cycles row has a single completed cycle inside
				// the dashboard's window, below the three-cycle floor, so the dashboard
				// rightly projects no window for it and the two empty lists agree.
				isControl := !testCase.wantPaused && !testCase.wholeHistoryPaused
				if isControl && len(fromDashboard) == 0 {
					t.Fatal("fixture: the dashboard projects no window over the swept days")
				}
				if isControl && len(fromFeedback) == 0 {
					t.Fatal("a history the dashboard projects never gets the fertile message")
				}
			})
		}
	})
}
