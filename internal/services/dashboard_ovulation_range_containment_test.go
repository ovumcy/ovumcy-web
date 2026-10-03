package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The fertile window the status is read against and the ovulation range the
// dashboard prints are two views of one irregular-mode projection, so the range
// must sit inside the window: a day the page names as a possible ovulation day
// may never be a day the status calls "outside the estimated window".
//
// Each case goes through the production pipeline end to end — the owner's
// baseline writes the window (irregularFertilityWindow), the dashboard context
// writes the range (DashboardOvulationRange) — over one set of stats, and the
// two are compared by calendar day in several request zones.

// irregularContainmentLogs records the cycle starts that give an irregular
// owner completed cycles of the given lengths, the running cycle starting on the
// last returned start.
func irregularContainmentLogs(t *testing.T, lengths []int) ([]models.DailyLog, time.Time) {
	t.Helper()

	start := mustParseDay(t, "2026-01-01")
	logs := []models.DailyLog{{Date: start, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium}}
	for _, length := range lengths {
		start = start.AddDate(0, 0, length)
		logs = append(logs, models.DailyLog{Date: start, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium})
	}
	return logs, start
}

func TestIrregularFertileWindowContainsTheDisplayedOvulationRange(t *testing.T) {
	zones := make(map[string]*time.Location, 4)
	for _, name := range []string{"UTC", "America/New_York", "Asia/Tokyo", "Pacific/Auckland"} {
		zones[name] = calendarDayComparisonZone(t, name)
	}

	for _, testCase := range []struct {
		name                 string
		lengths              []int
		luteal               int
		wantMin, wantMax     int
		wantFirstRangeCycleD int
	}{
		{name: "an ordinary spread", lengths: []int{24, 30, 36}, luteal: 14, wantMin: 24, wantMax: 36, wantFirstRangeCycleD: 10},
		{name: "a personal luteal phase", lengths: []int{26, 30, 34}, luteal: 10, wantMin: 26, wantMax: 34, wantFirstRangeCycleD: 16},
		// ovulation(16) = 16 - 14 = day 2, which the model lifts to cycle day 5:
		// the range starts on day 5 and the window's own start (day 5 - 5) is
		// clamped to cycle day 1.
		{name: "a shortest cycle whose ovulation clamps to cycle day 5", lengths: []int{16, 30, 36}, luteal: 14, wantMin: 16, wantMax: 36, wantFirstRangeCycleD: 5},
		// A shortest cycle under the floor cannot place an ovulation: the window
		// opens on the clamp (cycle day 1) and the range on the model's earliest.
		{name: "a shortest cycle under the floor", lengths: []int{12, 30, 36}, luteal: 14, wantMin: 12, wantMax: 36, wantFirstRangeCycleD: 5},
		{name: "a shortest cycle exactly at the floor", lengths: []int{15, 30, 36}, luteal: 10, wantMin: 15, wantMax: 36, wantFirstRangeCycleD: 5},
	} {
		for zoneName, location := range zones {
			t.Run(testCase.name+"/"+zoneName, func(t *testing.T) {
				logs, runningStart := irregularContainmentLogs(t, testCase.lengths)
				user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: testCase.luteal, IrregularCycle: true}

				now := localNoon(runningStart.AddDate(0, 0, 2), location)
				today := DateAtLocation(now, location)
				stats := BuildCycleStatsFromLogs(user, logs, now, location)
				if stats.MinCycleLength != testCase.wantMin || stats.MaxCycleLength != testCase.wantMax {
					t.Fatalf("fixture: observed lengths min/max = %d/%d, want %d/%d", stats.MinCycleLength, stats.MaxCycleLength, testCase.wantMin, testCase.wantMax)
				}

				context := BuildDashboardCycleContext(user, logs, stats, today, location)
				if !context.DisplayOvulationUseRange {
					t.Fatal("fixture: the dashboard must show an ovulation range for this account, or the containment below is vacuous")
				}
				if stats.FertilityWindowStart.IsZero() || stats.FertilityWindowEnd.IsZero() {
					t.Fatal("fixture: the stats carry no fertile window")
				}
				// Reported beside the containment checks below, not ahead of them: it
				// pins the case to the clamp or the floor it is named for, and a range
				// that moved off that day would otherwise hide whether it also left
				// the window.
				if got := CalendarDaysBetween(runningStart, context.DisplayOvulationRangeStart) + 1; got != testCase.wantFirstRangeCycleD {
					t.Errorf("fixture: the range opens on cycle day %d, want %d", got, testCase.wantFirstRangeCycleD)
				}

				if CalendarDaysBetween(stats.FertilityWindowStart, context.DisplayOvulationRangeStart) < 0 {
					t.Errorf("the range opens on %s, before the fertile window opens on %s",
						CalendarDayKey(context.DisplayOvulationRangeStart), CalendarDayKey(stats.FertilityWindowStart))
				}
				if CalendarDaysBetween(context.DisplayOvulationRangeEnd, stats.FertilityWindowEnd) < 0 {
					t.Errorf("the range closes on %s, after the fertile window closes on %s",
						CalendarDayKey(context.DisplayOvulationRangeEnd), CalendarDayKey(stats.FertilityWindowEnd))
				}
			})
		}
	}
}
