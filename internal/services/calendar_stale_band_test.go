package services

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
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
// the overdue gate (day 36). The next period was due on 2026-04-23, and the
// cycle chained after it starts on 2026-05-21 — a day still AHEAD of today, which
// is the one a suppression that spared only the past would withhold.
func TestCalendarGridKeepsProjectingInTheOutOfDateBand(t *testing.T) {
	user := dayFeedbackParityUser(64)
	logs := cycleStartLogs(t, "2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26")

	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		now := localNoon(mustParseDay(t, "2026-04-24"), location)
		today := DateAtLocation(now, location)
		stats := BuildCycleStatsFromLogs(user, logs, now, location)

		// Fixtures: the band is stale and not suppressed, or the grid's answer below
		// could come from the suppression gate instead.
		published, verdict := PublishedStats(user, stats, logs, today, location)
		if !published.CycleDataStale {
			t.Fatal("fixture: cycle day 30 must carry the out-of-date verdict")
		}
		if PredictionsSuppressed(user, stats) || verdict.PredictionsSuppressed || verdict.FertilitySuppressed {
			t.Fatal("fixture: cycle day 30 must be out of date without being suppressed")
		}

		predicted := map[string]bool{}
		ovulation := 0
		for _, monthStart := range []time.Time{
			time.Date(2026, time.April, 1, 0, 0, 0, 0, location),
			time.Date(2026, time.May, 1, 0, 0, 0, 0, location),
		} {
			for _, day := range BuildCalendarDayStates(user, monthStart, logs, stats, now, location) {
				if !day.InMonth {
					continue
				}
				if day.IsPredicted {
					predicted[day.DateString] = true
				}
				if day.IsOvulation && day.Date.After(today) {
					ovulation++
				}
			}
		}
		for _, want := range []string{"2026-04-23", "2026-05-21"} {
			if !predicted[want] {
				t.Fatalf("the grid keeps drawing the projected period start %s while the data is out of date; predicted days: %v", want, predicted)
			}
		}
		if ovulation == 0 {
			t.Fatal("the grid keeps drawing a projected ovulation day ahead of today while the data is out of date")
		}
	})
}

// TestCalendarPageViewDoesNotPublishTheOutOfDateVerdictToTheGrid is the behavioural
// half of the same decision. CalendarPageViewData.Stats is a CycleStats, which has
// a CycleDataStale field, so a declaration check cannot show the page carries no
// verdict: this one builds the page from stale-band stats and requires the field
// to be false, while the same stats published through PublishedStats say true.
// Today only PublishedStats writes the field; a page that started publishing its
// output would print the banner and (by the dashboard's own rules) take the
// decision above with it.
func TestCalendarPageViewDoesNotPublishTheOutOfDateVerdictToTheGrid(t *testing.T) {
	user := dayFeedbackParityUser(66)
	user.Role = models.RoleOwner
	logs := cycleStartLogs(t, "2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26")

	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		now := localNoon(mustParseDay(t, "2026-04-24"), location)
		stats := BuildCycleStatsFromLogs(user, logs, now, location)
		if published, _ := PublishedStats(user, stats, logs, DateAtLocation(now, location), location); !published.CycleDataStale {
			t.Fatal("control: the same stats published through PublishedStats carry the out-of-date verdict")
		}

		service := NewCalendarViewService(&stubCalendarViewDayReader{logs: logs}, &stubCalendarViewStatsProvider{stats: stats, logs: logs})
		view, err := service.BuildCalendarPageViewData(context.Background(), user, "en", now, time.Date(2026, time.April, 1, 0, 0, 0, 0, location), "", location)
		if err != nil {
			t.Fatalf("BuildCalendarPageViewData: %v", err)
		}
		if view.Stats.CycleDataStale {
			t.Fatal("the calendar page model must not carry the out-of-date verdict")
		}
	})
}

// calendarStaleBandProjectionWords are the substrings the two declaration checks
// below look for, each anchored by a field that is KNOWN to exist, so a word that
// matches nothing anywhere cannot make a check read as stronger than it is.
var calendarStaleBandProjectionWords = map[string]struct {
	model reflect.Type
	field string
}{
	"predict": {reflect.TypeOf(CalendarDayState{}), "IsPredicted"},
	"fertil":  {reflect.TypeOf(CalendarDayState{}), "IsFertility"},
	"ovulat":  {reflect.TypeOf(CalendarDayState{}), "IsOvulation"},
	"stale":   {reflect.TypeOf(DashboardCycleContext{}), "CycleDataStale"},
}

func calendarStaleBandNamesProjection(t *testing.T, name string) bool {
	t.Helper()
	lower := strings.ToLower(name)
	for word, anchor := range calendarStaleBandProjectionWords {
		if _, ok := anchor.model.FieldByName(anchor.field); !ok {
			t.Fatalf("anchor: %s must have a field %s", anchor.model.Name(), anchor.field)
		}
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// calendarStaleBandFieldPaths lists the dotted name of every struct field reachable
// from model through struct, pointer, slice and array element types, so a nested
// type's fields are read too. time.Time is a leaf.
func calendarStaleBandFieldPaths(model reflect.Type, prefix string, seen map[reflect.Type]bool) []string {
	for model.Kind() == reflect.Pointer || model.Kind() == reflect.Slice || model.Kind() == reflect.Array {
		model = model.Elem()
	}
	if model.Kind() != reflect.Struct || model == reflect.TypeOf(time.Time{}) || seen[model] {
		return nil
	}
	seen[model] = true
	var paths []string
	for i := range model.NumField() {
		field := model.Field(i)
		path := prefix + field.Name
		paths = append(paths, path)
		paths = append(paths, calendarStaleBandFieldPaths(field.Type, path+".", seen)...)
	}
	return paths
}

// TestCalendarDayPanelModelCarriesNoPredictionSignal pins what the day panel
// (/calendar/day/:date) is: a view of ONE recorded day, built from that day's log
// and the cycle-start policy. No field reachable from its view model — nested
// structs included — names a predicted day, a fertility or ovulation claim or the
// out-of-date verdict, so the panel has nothing for those states to change. The
// check is by field NAME over the service model, not over the handler's template
// payload; it fails the day such a field is added, which is when the panel needs
// its own gate.
func TestCalendarDayPanelModelCarriesNoPredictionSignal(t *testing.T) {
	paths := calendarStaleBandFieldPaths(reflect.TypeOf(DayEditorViewData{}), "DayEditorViewData.", map[reflect.Type]bool{})
	if len(paths) < 20 {
		t.Fatalf("anchor: the walk must reach the model's fields, found %d", len(paths))
	}
	nested := false
	for _, path := range paths {
		if strings.HasPrefix(path, "DayEditorViewData.Log.") {
			nested = true
		}
		if calendarStaleBandNamesProjection(t, path[strings.LastIndex(path, ".")+1:]) {
			t.Fatalf("%s names a projection signal; the day panel was decided to carry none", path)
		}
	}
	if !nested {
		t.Fatal("anchor: the walk must descend into the nested day log")
	}
}

// TestCalendarGridCellAndPageModelNameNoOutOfDateField covers TOP-LEVEL field names
// only: CalendarDayState and CalendarPageViewData declare nothing called stale. It
// does not prove the page carries no verdict — CalendarPageViewData.Stats is a
// CycleStats whose own CycleDataStale field this walk does not enter; that is the
// behavioural test above.
func TestCalendarGridCellAndPageModelNameNoOutOfDateField(t *testing.T) {
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
	if !calendarStaleBandNamesProjection(t, "CycleDataStale") {
		t.Fatal("anchor: the predicate must recognise a stale field name")
	}
}
