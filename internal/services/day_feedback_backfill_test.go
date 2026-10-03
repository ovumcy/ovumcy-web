package services

import (
	"testing"
	"time"
)

// The fertile line is spoken only when the day being saved is today, whatever
// window the current cycle projects over it: a day filled in after the fact and
// a day still ahead both get the neutral message.
//
// The history is three 28-day cycles, so the running cycle (started 2026-03-26)
// projects its default-luteal window on cycle days 9-14, 2026-04-03..04-08.
func TestDayFeedbackIsNeutralForABackfilledDayInsideTheWindow(t *testing.T) {
	user := dayFeedbackParityUser(71)
	logs := cycleStartLogs(t, "2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26")

	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		today := mustParseDay(t, "2026-04-05") // cycle day 11, inside the window

		for _, testCase := range []struct {
			name  string
			day   string
			today string
			want  string
		}{
			{"today", "2026-04-05", "2026-04-05", daySaveMessageFertile},
			{"a past day inside the window", "2026-04-03", "2026-04-05", daySaveMessageNeutral},
			{"yesterday, the last day behind today", "2026-04-04", "2026-04-05", daySaveMessageNeutral},
			{"a future day inside the window", "2026-04-07", "2026-04-05", daySaveMessageNeutral},
			{"tomorrow, the first day ahead", "2026-04-06", "2026-04-05", daySaveMessageNeutral},
			{"the window's last day, saved as today", "2026-04-08", "2026-04-08", daySaveMessageFertile},
			{"the window's last day, backfilled the next day", "2026-04-08", "2026-04-09", daySaveMessageNeutral},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				if got := dayFeedbackKeyOn(t, user, logs, location, mustParseDay(t, testCase.today), mustParseDay(t, testCase.day)); got != testCase.want {
					t.Fatalf("saving %s on %s resolves to %q, want %q", testCase.day, testCase.today, got, testCase.want)
				}
			})
		}

		// Fixture: the past day is inside the window the dashboard shades, so only
		// the backfill rule can be what answers neutral above.
		fromDashboard := dashboardFertileDays(t, user, logs, location, today, mustParseDay(t, "2026-04-01"), mustParseDay(t, "2026-04-10"))
		if len(fromDashboard) == 0 || fromDashboard[0] != "2026-04-03" {
			t.Fatalf("fixture: the dashboard window covers %v, want it to start on 2026-04-03", fromDashboard)
		}
	})
}
