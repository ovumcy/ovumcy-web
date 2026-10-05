package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-209 round 3: the three-completed-cycle floor withholds projections only. A
// completed past cycle has its own record, so the "show historical phases" marks
// on it — the calendar's ovulation and fertile cells and the stats cycle stack's
// inferred phases — stay below the floor, while the current cycle's projection is
// withheld. The control is the same history with a regular account at three
// cycles, whose current cycle does carry its projection.

func retrospectiveFloorHistory(today time.Time, startsDaysAgo ...int) (*models.User, []models.DailyLog) {
	user := firstCycleFloorUser()
	user.ShowHistoricalPhases = true
	starts := make([]time.Time, 0, len(startsDaysAgo))
	for _, daysAgo := range startsDaysAgo {
		starts = append(starts, today.AddDate(0, 0, -daysAgo))
	}
	return user, firstCycleFloorLogs(starts)
}

func TestFloorWithholdsProjectionsOnlyAndPastCyclesKeepTheirInferredShading(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 4, 20, 9, 0, 0, 0, location)
	today := DateAtLocation(now, location)
	currentStart := today.AddDate(0, 0, -12)
	// The cycle that started 40 days ago closed 12 days ago after 28 days, so its
	// ovulation (luteal phase 14) is on its day 14: 27 days ago.
	pastOvulationDay := today.AddDate(0, 0, -27)

	for _, testCase := range []struct {
		name               string
		startsDaysAgo      []int
		wantCompleted      int
		wantCurrentProject bool
	}{
		{name: "two completed cycles", startsDaysAgo: []int{68, 40, 12}, wantCompleted: 2},
		{name: "control: three completed cycles project the current cycle", startsDaysAgo: []int{96, 68, 40, 12}, wantCompleted: 3, wantCurrentProject: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			user, logs := retrospectiveFloorHistory(today, testCase.startsDaysAgo...)
			stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(user, logs, now, location)
			if stats.CompletedCycleCount != testCase.wantCompleted {
				t.Fatalf("fixture: %d completed cycles, want %d", stats.CompletedCycleCount, testCase.wantCompleted)
			}
			if got := FertilityProjectionSuppressed(user, stats); got == testCase.wantCurrentProject {
				t.Fatalf("fixture: FertilityProjectionSuppressed = %v with %d completed cycles", got, testCase.wantCompleted)
			}

			// Every month the history and the current cycle's projection touch is
			// read, not only the one holding the ovulation day.
			months := map[time.Time]bool{}
			for _, daysAgo := range testCase.startsDaysAgo {
				day := today.AddDate(0, 0, -daysAgo)
				months[time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, location)] = true
			}
			months[time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, location)] = true
			months[time.Date(today.Year(), today.Month()+1, 1, 0, 0, 0, 0, location)] = true

			sawPastOvulation := false
			pastMarks, currentMarks := 0, 0
			for monthStart := range months {
				for _, cell := range BuildCalendarDayStates(user, monthStart, logs, stats, now, location) {
					marked := cell.IsOvulation || cell.IsFertility || cell.IsFertilityPeak || cell.IsFertilityEdge
					if !marked || !cell.InMonth {
						continue
					}
					if cell.Date.Before(currentStart) {
						pastMarks++
						if CalendarDayKey(cell.Date) == CalendarDayKey(pastOvulationDay) && cell.IsOvulation {
							sawPastOvulation = true
						}
					} else {
						currentMarks++
					}
				}
			}
			if !sawPastOvulation || pastMarks == 0 {
				t.Fatalf("a past cycle must keep its ovulation day %s and fertile shading: ovulation=%v marks=%d", CalendarDayKey(pastOvulationDay), sawPastOvulation, pastMarks)
			}
			if testCase.wantCurrentProject != (currentMarks > 0) {
				t.Fatalf("the current cycle's projection: %d marked cells, want projected=%v", currentMarks, testCase.wantCurrentProject)
			}

			ribbon := buildStatsCycleRibbon(user, stats, logs, buildCompletedCycleSpans(logs, location))
			fertile, peak, ovulation := statscycleribbonInferredFertility(ribbon)
			if !ribbon.Visible || !ribbon.ShowPhases || fertile == 0 || peak == 0 || ovulation == 0 {
				t.Fatalf("the stats ribbon must keep its inferred phases: visible=%v phases=%v fertile=%d peak=%d ovulation=%d", ribbon.Visible, ribbon.ShowPhases, fertile, peak, ovulation)
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

			// The published stats already clear the window below the floor, so the
			// end-to-end row above would stay green if the policy's own gate were
			// dropped. Handing the policy the UNPUBLISHED stats, which still carry
			// the window, makes the verdict the only thing standing between today and
			// the fertile line.
			if stats.FertilityWindowStart.IsZero() {
				t.Fatal("fixture: the unpublished stats must carry the window the verdict is withholding")
			}
			suppression := ResolvePredictionSuppression(user, stats)
			if got := resolveDaySaveMessageKey(user, today, today, stats, suppression); got != testCase.want {
				t.Fatalf("the policy with the window in hand resolves to %q with %d completed cycles, want %q", got, testCase.completed, testCase.want)
			}
		})
	}
}
