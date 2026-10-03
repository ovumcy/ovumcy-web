package services

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestCalendarGridKeepsProjectingInTheOutOfDateBand pins a DECISION, not an
// oversight: between the account's reference cycle length and the overdue gate
// (a further week) the dashboard, /stats and the JSON API print "unknown" for the
// phase and the fertility status and show the out-of-date banner, while the
// calendar grid keeps drawing the projected days. The out-of-date verdict
// withholds the phase and the status and nothing else (PublishedStats), the grid
// shows no phase or status, and the projected dates it does draw sit beside the
// same dates the other pages still publish. Moving the verdict into the grid is a
// separate product decision; this test turns it from an unpinned omission into a
// stated one.
//
// The history is three 28-day cycles and a running one from 2026-03-26, so the
// reference length is 28 and cycle day 30 (2026-04-24) is out of date yet short of
// the overdue gate (day 36).
func TestCalendarGridKeepsProjectingInTheOutOfDateBand(t *testing.T) {
	user := dayFeedbackParityUser(64)
	logs := cycleStartLogs(t, "2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26")
	now := localNoon(mustParseDay(t, "2026-04-24"), time.UTC)
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)

	// Fixtures: the band is stale and not suppressed, or the grid's answer below
	// could come from the suppression gate instead.
	published, verdict := PublishedStats(user, stats, logs, today, time.UTC)
	if !published.CycleDataStale {
		t.Fatal("fixture: cycle day 30 must carry the out-of-date verdict")
	}
	if PredictionsSuppressed(user, stats) || verdict.PredictionsSuppressed || verdict.FertilitySuppressed {
		t.Fatal("fixture: cycle day 30 must be out of date without being suppressed")
	}

	predicted, fertile, ovulation := 0, 0, 0
	for _, monthStart := range []time.Time{
		time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
	} {
		for _, day := range BuildCalendarDayStates(user, monthStart, logs, stats, now, time.UTC) {
			if !day.InMonth {
				continue
			}
			if day.IsPredicted {
				predicted++
			}
			if day.IsFertility {
				fertile++
			}
			if day.IsOvulation {
				ovulation++
			}
		}
	}
	if predicted == 0 || fertile == 0 || ovulation == 0 {
		t.Fatalf("the grid keeps drawing the projection while the data is out of date; got predicted=%d fertile=%d ovulation=%d", predicted, fertile, ovulation)
	}
}

// TestCalendarViewModelsCarryNoOutOfDateSignal states the other half of the same
// decision: nothing in the grid cell or the calendar page model names the
// out-of-date verdict, so the page cannot print it. The check is by declaration —
// every field of both types — and fails the day a field naming staleness is
// added, which is the moment the decision above has been reversed and this pair
// of tests should be rewritten with it.
func TestCalendarViewModelsCarryNoOutOfDateSignal(t *testing.T) {
	for _, model := range []reflect.Type{
		reflect.TypeOf(CalendarDayState{}),
		reflect.TypeOf(CalendarPageViewData{}),
	} {
		for i := range model.NumField() {
			if name := model.Field(i).Name; strings.Contains(strings.ToLower(name), "stale") {
				t.Fatalf("%s.%s names the out-of-date verdict; the calendar page was decided to carry none", model.Name(), name)
			}
		}
	}

	// Anti-vacuity: the dashboard context does carry the verdict, and the same
	// check finds it there.
	if _, ok := reflect.TypeOf(DashboardCycleContext{}).FieldByName("CycleDataStale"); !ok {
		t.Fatal("anchor: the dashboard cycle context must carry CycleDataStale")
	}
}
