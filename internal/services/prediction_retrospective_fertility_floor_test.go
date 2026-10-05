package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-209 round 2: the three-completed-cycle floor withholds retrospective
// fertility marks as well. A past cycle's window is the same cycle arithmetic as a
// future one, so with one or two completed cycles behind the account the calendar's
// historical pass and the stats stack's inferred phases read the one fertility
// gate, and a regular owner with ShowHistoricalPhases on sees neither. The
// control is the same history one cycle longer: the marks are there, so the
// withheld rows are not green against a surface that cannot paint them at all.

func retrospectiveFloorHistory(today time.Time, startsDaysAgo ...int) (*models.User, []models.DailyLog) {
	user := firstCycleFloorUser()
	user.ShowHistoricalPhases = true
	starts := make([]time.Time, 0, len(startsDaysAgo))
	for _, daysAgo := range startsDaysAgo {
		starts = append(starts, today.AddDate(0, 0, -daysAgo))
	}
	return user, firstCycleFloorLogs(starts)
}

func TestCalendarHistoricalFertilityMarksFollowTheThreeCycleFloor(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 4, 20, 9, 0, 0, 0, location)
	today := DateAtLocation(now, location)
	// The cycle that started 40 days ago closed 12 days ago after 28 days, so its
	// ovulation (luteal phase 14) is on its day 14: 27 days ago.
	ovulationDay := today.AddDate(0, 0, -27)

	for _, testCase := range []struct {
		name          string
		startsDaysAgo []int
		wantCompleted int
		wantMarks     bool
	}{
		{name: "two completed cycles: no retrospective fertility", startsDaysAgo: []int{68, 40, 12}, wantCompleted: 2},
		{name: "one completed cycle: no retrospective fertility", startsDaysAgo: []int{40, 12}, wantCompleted: 1},
		{name: "control: three completed cycles keep the marks", startsDaysAgo: []int{96, 68, 40, 12}, wantCompleted: 3, wantMarks: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			user, logs := retrospectiveFloorHistory(today, testCase.startsDaysAgo...)
			stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(user, logs, now, location)
			if stats.CompletedCycleCount != testCase.wantCompleted {
				t.Fatalf("fixture: %d completed cycles, want %d", stats.CompletedCycleCount, testCase.wantCompleted)
			}

			// Every month a past cycle touches is read, not only the one holding the
			// ovulation day, so a window spilling into a neighbouring month is seen.
			months := map[time.Time]bool{}
			for _, daysAgo := range testCase.startsDaysAgo {
				day := today.AddDate(0, 0, -daysAgo)
				months[time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, location)] = true
			}
			marked := 0
			sawOvulationDay := false
			for monthStart := range months {
				for _, cell := range BuildCalendarDayStates(user, monthStart, logs, stats, now, location) {
					if cell.IsOvulation || cell.IsFertility || cell.IsFertilityPeak || cell.IsFertilityEdge || cell.IsPreFertile {
						marked++
					}
					if cell.InMonth && CalendarDayKey(cell.Date) == CalendarDayKey(ovulationDay) && cell.IsOvulation {
						sawOvulationDay = true
					}
				}
			}
			if testCase.wantMarks {
				if !sawOvulationDay {
					t.Fatalf("control: the closed cycle's ovulation day %s must carry the marker", CalendarDayKey(ovulationDay))
				}
				return
			}
			if marked != 0 || sawOvulationDay {
				t.Fatalf("calendar painted %d retrospective fertility cells (ovulation day marked: %v) below the floor", marked, sawOvulationDay)
			}
		})
	}
}

func TestStatsRibbonInferredFertilityFollowsTheThreeCycleFloor(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 4, 20, 9, 0, 0, 0, location)
	today := DateAtLocation(now, location)

	for _, testCase := range []struct {
		name          string
		startsDaysAgo []int
		wantCompleted int
		wantMarks     bool
	}{
		{name: "two completed cycles: the ribbon names no fertile cell", startsDaysAgo: []int{68, 40, 12}, wantCompleted: 2},
		{name: "control: three completed cycles shade the fertile window and peak", startsDaysAgo: []int{96, 68, 40, 12}, wantCompleted: 3, wantMarks: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			user, logs := retrospectiveFloorHistory(today, testCase.startsDaysAgo...)
			stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(user, logs, now, location)
			if stats.CompletedCycleCount != testCase.wantCompleted {
				t.Fatalf("fixture: %d completed cycles, want %d", stats.CompletedCycleCount, testCase.wantCompleted)
			}

			ribbon := buildStatsCycleRibbon(user, stats, logs, buildCompletedCycleSpans(logs, location))
			if !ribbon.Visible {
				t.Fatal("recorded cycles are facts: the ribbon stays visible below the floor")
			}
			fertile, peak, _ := statscycleribbonInferredFertility(ribbon)
			if testCase.wantMarks {
				if !ribbon.ShowPhases || fertile == 0 || peak == 0 {
					t.Fatalf("control: ShowPhases=%v fertile=%d peak=%d, want phases with a fertile window and a peak", ribbon.ShowPhases, fertile, peak)
				}
				return
			}
			if ribbon.ShowPhases || fertile != 0 || peak != 0 {
				t.Fatalf("below the floor: ShowPhases=%v fertile=%d peak=%d, want none", ribbon.ShowPhases, fertile, peak)
			}
		})
	}
}

// TestDayFeedbackFertileMessageFollowsTheThreeCycleFloor drives the day-save toast
// for a regular account saving today, which is cycle day 13 — inside the default
// luteal window of a 28-day cycle (days 9-14). The window exists in every row; only
// the completed-cycle count differs, so the neutral answer below the floor can only
// come from the fertility gate, and the three-cycle control shows the fertile line
// is reachable.
func TestDayFeedbackFertileMessageFollowsTheThreeCycleFloor(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, location)
	today := DateAtLocation(now, location)

	for _, testCase := range []struct {
		name      string
		completed int
		want      string
	}{
		{name: "one completed cycle", completed: 1, want: daySaveMessageNeutral},
		{name: "two completed cycles", completed: 2, want: daySaveMessageNeutral},
		{name: "control: three completed cycles", completed: 3, want: daySaveMessageFertile},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			user, logs := awaitingMoreCyclesHistory(today, testCase.completed)
			stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(user, logs, now, location)
			if stats.CompletedCycleCount != testCase.completed {
				t.Fatalf("fixture: %d completed cycles, want %d", stats.CompletedCycleCount, testCase.completed)
			}
			if got := dayFeedbackKeyOn(t, user, logs, location, today, today); got != testCase.want {
				t.Fatalf("saving today with %d completed cycles resolves to %q, want %q", testCase.completed, got, testCase.want)
			}
		})
	}
}
