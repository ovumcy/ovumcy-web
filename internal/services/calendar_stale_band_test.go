package services

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestCalendarGridKeepsTheNextPeriodButNoFertileDayInTheOutOfDateBand pins a
// DECISION: between the account's reference cycle length and the overdue gate
// (a further week) the fertility half of the projection is withheld on every
// surface — the out-of-date band is a fertility suppression signal
// (DashboardCyclePastReferenceLength) — while the projected period days stay,
// on the grid as on the other pages, until the overdue gate withholds them too.
// An ovulation drawn here would belong to a cycle chained from a start nobody
// logged, beside the late-cycle notice the same pages show from that day.
//
// The history is three 28-day cycles and a running one from 2026-03-26, so the
// reference length is 28 and cycle day 30 (2026-04-24) is out of date yet short of
// the overdue gate (day 36). The next period was due on 2026-04-23, and the
// cycle chained after it starts on 2026-05-21 — a day still AHEAD of today, which
// is the one a suppression that spared only the past would withhold.
func TestCalendarGridKeepsTheNextPeriodButNoFertileDayInTheOutOfDateBand(t *testing.T) {
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
		if PredictionsSuppressed(user, stats) || verdict.PredictionsSuppressed {
			t.Fatal("fixture: cycle day 30 must be out of date without the overdue gate firing")
		}
		if !verdict.FertilitySuppressed {
			t.Fatal("the out-of-date band must withhold the fertility half")
		}

		predicted := map[string]bool{}
		ovulation := 0
		fertile := 0
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
				if day.IsFertility && day.Date.After(today) {
					fertile++
				}
			}
		}
		for _, want := range []string{"2026-04-23", "2026-05-21"} {
			if !predicted[want] {
				t.Fatalf("the grid keeps drawing the projected period start %s while the data is out of date; predicted days: %v", want, predicted)
			}
		}
		if ovulation != 0 || fertile != 0 {
			t.Fatalf("the grid drew %d projected ovulation and %d fertile day(s) ahead of today while the data is out of date", ovulation, fertile)
		}
	})
}

// TestConfirmedOvulationSurvivesTheOutOfDateBandOnEverySurface pins the other half
// of the decision above: the out-of-date band withholds the fertility PROJECTION,
// and a day the owner's own temperatures confirmed is not a projection. The gate
// that lets such a day through (ConfirmedOvulationWithheld) is the fertility gate
// without the staleness signal, so on the same history as the grid test above
// (three 28-day cycles, the running one from 2026-03-26, cycle day 30 on
// 2026-04-24) a thermal shift on days 14-16 names ovulation on cycle day 13,
// 2026-04-07, and every surface keeps that one day:
//   - the grid carries a solid marker on it and no other ovulation marker, tentative
//     or solid, and no fertile, peak, edge or pre-fertile day anywhere from the
//     cycle start on;
//   - the dashboard names it as confirmed;
//   - the overview publishes it as confirmed, with no window and an unknown status
//     (the window and status derived from a day stay behind the fertility gate).
//
// The projected ovulation, 2026-04-08, is a day later: a surface that fell back to
// the projection instead of the shift would be told apart.
func TestConfirmedOvulationSurvivesTheOutOfDateBandOnEverySurface(t *testing.T) {
	user := dayFeedbackParityUser(72)
	logs := cycleStartLogs(t, "2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26")
	lastStart := mustParseDay(t, "2026-03-26")
	for cycleDay := 8; cycleDay <= 20; cycleDay++ {
		reading := 36.20
		if cycleDay >= 14 {
			reading = 36.50
		}
		logs = append(logs, models.DailyLog{Date: AddCalendarDays(lastStart, cycleDay-1, time.UTC), BBT: &reading})
	}
	const wantKey, projectedKey = "2026-04-07", "2026-04-08"

	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		now := localNoon(mustParseDay(t, "2026-04-24"), location)
		today := DateAtLocation(now, location)
		stats := ApplyUserCycleBaseline(user, logs, BuildCycleStatsFromLogs(user, logs, now, location), today, location)

		// Fixtures: out of date by the fertility signal alone, so the markers below
		// come from the confirmed day and not from a gate that was never closed.
		if stats.CurrentCycleDay != 30 || !DashboardCyclePastReferenceLength(user, stats) || !FertilityProjectionSuppressed(user, stats) {
			t.Fatalf("fixture: cycle day %d, past reference %t, fertility suppressed %t, want cycle day 30 out of date",
				stats.CurrentCycleDay, DashboardCyclePastReferenceLength(user, stats), FertilityProjectionSuppressed(user, stats))
		}
		if PredictionsSuppressed(user, stats) || DashboardCycleOverdue(user, stats) {
			t.Fatal("fixture: cycle day 30 must be out of date without the overdue gate firing")
		}
		if got := CalendarDayKey(stats.OvulationDate); got != projectedKey {
			t.Fatalf("fixture: the projected ovulation = %s, want %s", got, projectedKey)
		}
		confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, location)
		if !ok || CalendarDayKey(confirmed) != wantKey {
			t.Fatalf("resolver: confirmed %s (ok=%t), want %s", CalendarDayKey(confirmed), ok, wantKey)
		}

		// The grid: one solid marker on the confirmed day, nothing fertile.
		var solid, tentative, fertileDays []string
		for _, monthStart := range []time.Time{
			time.Date(2026, time.March, 1, 0, 0, 0, 0, location),
			time.Date(2026, time.April, 1, 0, 0, 0, 0, location),
			time.Date(2026, time.May, 1, 0, 0, 0, 0, location),
		} {
			for _, day := range BuildCalendarDayStates(user, monthStart, logs, stats, now, location) {
				if !day.InMonth || CalendarDay(day.Date, location).Before(lastStart) {
					continue
				}
				if day.IsOvulation {
					solid = append(solid, day.DateString)
				}
				if day.IsTentativeOvulation {
					tentative = append(tentative, day.DateString)
				}
				if day.IsFertility || day.IsFertilityPeak || day.IsFertilityEdge || day.IsPreFertile || day.IsPredictedFertileOverlap {
					fertileDays = append(fertileDays, day.DateString)
				}
			}
		}
		if !slices.Equal(solid, []string{wantKey}) || len(tentative) != 0 {
			t.Errorf("calendar: solid %v, tentative %v, want the confirmed %s alone", solid, tentative, wantKey)
		}
		if len(fertileDays) != 0 {
			t.Errorf("calendar: fertile, peak, edge or pre-fertile days %v on out-of-date data, want none", fertileDays)
		}

		// The dashboard.
		cycleContext := BuildDashboardCycleContext(user, logs, stats, today, location)
		if !cycleContext.FertilitySuppressed || CalendarDayKey(cycleContext.DisplayOvulationDate) != wantKey || !cycleContext.DisplayOvulationConfirmed {
			t.Errorf("dashboard: fertility suppressed %t, ovulation %q, confirmed %t, want the confirmed %s beside a suppressed projection",
				cycleContext.FertilitySuppressed, CalendarDayKey(cycleContext.DisplayOvulationDate), cycleContext.DisplayOvulationConfirmed, wantKey)
		}

		// The overview.
		published, suppression, confirmedOvulation := PublishedOverviewStats(user, logs, stats, today, location)
		if suppression.PredictionsSuppressed || !suppression.FertilitySuppressed || !slices.Contains(suppression.Reasons, SuppressionReasonCycleDataStale) {
			t.Errorf("API: suppression %+v, want the fertility half withheld as out of date and nothing more", suppression)
		}
		if CalendarDayKey(published.OvulationDate) != wantKey || !confirmedOvulation {
			t.Errorf("API: ovulation %q confirmed %t, want the confirmed %s", CalendarDayKey(published.OvulationDate), confirmedOvulation, wantKey)
		}
		if !published.FertilityWindowStart.IsZero() || !published.FertilityWindowEnd.IsZero() {
			t.Errorf("API: window %s..%s, want none — the window derived from the confirmed day stays withheld",
				CalendarDayKey(published.FertilityWindowStart), CalendarDayKey(published.FertilityWindowEnd))
		}
		if !published.CycleDataStale || published.NextPeriodStart.IsZero() {
			t.Errorf("API: stale %t, next period %q, want the out-of-date verdict beside the running cycle's own next period",
				published.CycleDataStale, CalendarDayKey(published.NextPeriodStart))
		}
		assertPublishedStatusAndPhase(t, published, FertilityStatusUnknown, "", "unknown")
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
		view, err := service.BuildCalendarPageViewData(context.Background(), user, "en", now, time.Date(2026, time.April, 1, 0, 0, 0, 0, location), "", CalendarMonthBounds{}, location)
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
