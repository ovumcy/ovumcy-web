package services

// prediction_skipped_midnight_surfaces_test.go — every surface that places a
// projected date must name the right day when that day is one whose local
// midnight the zone skips (America/Santiago 2026-09-06, America/Havana
// 2026-03-08). A step taken with AddDate inside the request zone resolves the
// missing wall clock backward and returns the PREVIOUS calendar day, so each
// case here projects a date ONTO the skipped day and reads it back from the
// surface: the .ics feed, the range resolver, the published overview stats, the
// webhook reminder decision and the dashboard context. Every zone case carries a
// UTC control on the same calendar dates, where the step cannot go wrong.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type skippedMidnightZone struct {
	name     string
	location *time.Location
	// skipped is the calendar day whose local midnight the zone does not have.
	skipped time.Time
}

func skippedMidnightZones(t *testing.T) []skippedMidnightZone {
	t.Helper()
	return []skippedMidnightZone{
		{name: "santiago", location: santiagoTestLocation(t), skipped: time.Date(2026, time.September, 6, 0, 0, 0, 0, time.UTC)},
		{name: "havana", location: havanaTestLocation(t), skipped: time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)},
		{name: "control: UTC", location: time.UTC, skipped: time.Date(2026, time.September, 6, 0, 0, 0, 0, time.UTC)},
	}
}

func (zone skippedMidnightZone) day(offset int) time.Time {
	return zone.skipped.AddDate(0, 0, offset)
}

func (zone skippedMidnightZone) key(offset int) string { return CalendarDayKey(zone.day(offset)) }

// compact is the YYYYMMDD form the .ics DTSTART/DTEND carry.
func (zone skippedMidnightZone) compact(offset int) string {
	return strings.ReplaceAll(zone.key(offset), "-", "")
}

// owner is a regular 28/5/14 owner whose latest cycle started lastStartOffset
// days from the skipped day, behind three more 28-day cycles (three completed
// cycles — the history the fertility half needs).
func (zone skippedMidnightZone) owner(lastStartOffset int) (*models.User, []models.DailyLog) {
	user := &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
	var logs []models.DailyLog
	for cycle := 3; cycle >= 0; cycle-- {
		logs = append(logs, models.DailyLog{Date: zone.day(lastStartOffset - 28*cycle), IsPeriod: true, CycleStart: true})
	}
	return user, logs
}

// at is local midday on the day offset days from the skipped day.
func (zone skippedMidnightZone) at(offset int) time.Time {
	year, month, day := zone.day(offset).Date()
	return time.Date(year, month, day, 12, 0, 0, 0, zone.location)
}

// The next period starts 28 days after the latest cycle start, so a latest start
// 28 days before the skipped day projects the next period onto it; the projected
// ovulation is 13 days after a cycle start, so a latest start 13 days before the
// skipped day puts the ovulation onto it.
const (
	periodOnSkippedDayStart    = -28
	ovulationOnSkippedDayStart = -13
)

func TestFeedNamesTheSkippedMidnightDayForProjectedPeriodAndOvulation(t *testing.T) {
	for _, zone := range skippedMidnightZones(t) {
		for _, testCase := range []struct {
			name      string
			lastStart int
			sinceLast int
			kind      string
		}{
			{name: "period", lastStart: periodOnSkippedDayStart, sinceLast: 11, kind: calendarFeedKindPeriod},
			{name: "ovulation", lastStart: ovulationOnSkippedDayStart, sinceLast: 5, kind: calendarFeedKindOvulation},
		} {
			t.Run(zone.name+"/"+testCase.name, func(t *testing.T) {
				user, logs := zone.owner(testCase.lastStart)
				input := CalendarFeedICSInput{
					User:       user,
					Logs:       logs,
					Now:        zone.at(testCase.lastStart + testCase.sinceLast),
					Location:   zone.location,
					Disclaimer: "estimate only",
				}
				body := string(buildTestFeedICS(input))

				want := fmt.Sprintf("DTSTART;VALUE=DATE:%s\r\nDTEND;VALUE=DATE:%s\r\n", zone.compact(0), zone.compact(1))
				if !strings.Contains(body, want) {
					t.Fatalf("the feed names no %s event on %s (DTEND %s):\n%s", testCase.name, zone.key(0), zone.key(1), body)
				}
				if previous := "DTSTART;VALUE=DATE:" + zone.compact(-1) + "\r\n"; strings.Contains(body, previous) {
					t.Fatalf("an event starts on %s, the day before the skipped midnight: the day step resolved backward\n%s", zone.key(-1), body)
				}
				// The event of the kind under test is the one on that day.
				if uid := feedTestUID(t, user.ID, testCase.kind, zone.compact(0)); !strings.Contains(body, "UID:"+uid) {
					t.Fatalf("the event on %s is not the %s one (UID %s missing):\n%s", zone.key(0), testCase.name, uid, body)
				}
			})
		}
	}
}

func TestResolveProjectionRangesNameTheSkippedMidnightDay(t *testing.T) {
	for _, zone := range skippedMidnightZones(t) {
		t.Run(zone.name+"/regular span", func(t *testing.T) {
			user := &models.User{ID: 1, Role: models.RoleOwner}
			stats := CycleStats{
				CompletedCycleCount: 4,
				CycleLengthStdDev:   2,
				LastPeriodStart:     CalendarDay(zone.day(-26), zone.location),
				LutealPhase:         14,
			}
			// The projected start is two days after the skipped day and the span two
			// days, so the range's first day IS the skipped day.
			ranges := ResolveProjectionRanges(user, stats, CalendarDay(zone.day(2), zone.location), zone.location)
			if !ranges.NextPeriodUseRange {
				t.Fatal("fixture: a regular owner with a spread must get a start range")
			}
			if got := CalendarDayKey(ranges.NextPeriodStart); got != zone.key(0) {
				t.Fatalf("range start = %s, want the skipped day %s", got, zone.key(0))
			}
			if got := CalendarDayKey(ranges.NextPeriodEnd); got != zone.key(4) {
				t.Fatalf("range end = %s, want %s", got, zone.key(4))
			}
		})
		t.Run(zone.name+"/irregular start range", func(t *testing.T) {
			user := &models.User{ID: 1, Role: models.RoleOwner, IrregularCycle: true}
			stats := CycleStats{
				CompletedCycleCount: 6,
				MinCycleLength:      28,
				MaxCycleLength:      35,
				LastPeriodStart:     CalendarDay(zone.day(periodOnSkippedDayStart), zone.location),
				LutealPhase:         14,
			}
			ranges := ResolveProjectionRanges(user, stats, CalendarDay(zone.day(3), zone.location), zone.location)
			if !ranges.NextPeriodUseRange {
				t.Fatal("fixture: an irregular owner with six cycles must get a start range")
			}
			if got := CalendarDayKey(ranges.NextPeriodStart); got != zone.key(0) {
				t.Fatalf("range start = %s, want the skipped day %s (last start + shortest cycle)", got, zone.key(0))
			}
			if got := CalendarDayKey(ranges.NextPeriodEnd); got != zone.key(7) {
				t.Fatalf("range end = %s, want %s (last start + longest cycle)", got, zone.key(7))
			}
		})
		t.Run(zone.name+"/irregular ovulation range", func(t *testing.T) {
			user := &models.User{ID: 1, Role: models.RoleOwner, IrregularCycle: true}
			stats := CycleStats{
				CompletedCycleCount: 6,
				MinCycleLength:      28,
				MaxCycleLength:      35,
				LastPeriodStart:     CalendarDay(zone.day(ovulationOnSkippedDayStart), zone.location),
				LutealPhase:         14,
			}
			ranges := ResolveProjectionRanges(user, stats, CalendarDay(zone.day(15), zone.location), zone.location)
			if !ranges.OvulationUseRange {
				t.Fatal("fixture: an irregular owner with six cycles must get an ovulation range")
			}
			if got := CalendarDayKey(ranges.OvulationStart); got != zone.key(0) {
				t.Fatalf("ovulation range start = %s, want the skipped day %s", got, zone.key(0))
			}
		})
	}
}

// The running cycle's ovulation is behind today, so the overview rolls the
// published ovulation and window a whole cycle forward: the projected cycle
// starts ON the skipped day and the window is chained from it.
func TestPublishedOverviewStatsRollOntoTheSkippedMidnightDay(t *testing.T) {
	published := func(zone skippedMidnightZone) CycleStats {
		user, logs := zone.owner(periodOnSkippedDayStart)
		today := DateAtLocation(zone.at(periodOnSkippedDayStart+16), zone.location)
		stats := BuildCycleStatsFromLogs(user, logs, today, zone.location)
		overview, _, _ := PublishedOverviewStats(user, logs, stats, today, zone.location)
		return overview
	}

	for _, zone := range skippedMidnightZones(t) {
		t.Run(zone.name, func(t *testing.T) {
			got := published(zone)
			if key := CalendarDayKey(got.NextPeriodStart); key != zone.key(0) {
				t.Fatalf("published next period = %s, want the skipped day %s", key, zone.key(0))
			}
			// The rolled ovulation is the projected cycle's: 13 days after the skipped day.
			if key := CalendarDayKey(got.OvulationDate); key != zone.key(13) {
				t.Fatalf("published ovulation = %s, want %s", key, zone.key(13))
			}
			if got.FertilityWindowStart.IsZero() || got.FertilityWindowEnd.IsZero() {
				t.Fatal("fixture: the rolled window must be published")
			}
			if start, end := CalendarDayKey(got.FertilityWindowStart), CalendarDayKey(got.FertilityWindowEnd); start != zone.key(8) || end != zone.key(13) {
				t.Fatalf("published window = %s..%s, want %s..%s (chained from the cycle that starts on the skipped day)", start, end, zone.key(8), zone.key(13))
			}
		})
	}
}

func TestWebhookReminderNamesTheSkippedMidnightDay(t *testing.T) {
	settings := enabledWebhookSettings(3)
	for _, zone := range skippedMidnightZones(t) {
		t.Run(zone.name+"/period", func(t *testing.T) {
			user, logs := zone.owner(periodOnSkippedDayStart)
			reminders := DecideDueReminders(user, settings, logs, zone.at(-2), zone.location)
			period, ok := findDueReminder(reminders, DueReminderTypePeriod)
			if !ok {
				t.Fatalf("no period reminder two days before the skipped day: %#v", reminders)
			}
			if got := CalendarDayKey(period.EventDate); got != zone.key(0) {
				t.Fatalf("period reminder event = %s, want the skipped day %s", got, zone.key(0))
			}
			if got := CalendarDayKey(period.CycleAnchor); got != zone.key(0) {
				t.Fatalf("period reminder anchor = %s, want the skipped day %s", got, zone.key(0))
			}
		})
		t.Run(zone.name+"/ovulation", func(t *testing.T) {
			user, logs := zone.owner(ovulationOnSkippedDayStart)
			reminders := DecideDueReminders(user, settings, logs, zone.at(-2), zone.location)
			ovulation, ok := findDueReminder(reminders, DueReminderTypeOvulation)
			if !ok {
				t.Fatalf("no ovulation reminder two days before the skipped day: %#v", reminders)
			}
			if got := CalendarDayKey(ovulation.EventDate); got != zone.key(0) {
				t.Fatalf("ovulation reminder event = %s, want the skipped day %s", got, zone.key(0))
			}
		})
	}
}

func TestDashboardContextNamesTheSkippedMidnightDay(t *testing.T) {
	for _, zone := range skippedMidnightZones(t) {
		t.Run(zone.name+"/next period", func(t *testing.T) {
			user, logs := zone.owner(periodOnSkippedDayStart)
			today := DateAtLocation(zone.at(periodOnSkippedDayStart+11), zone.location)
			stats := BuildCycleStatsFromLogs(user, logs, today, zone.location)
			context := BuildDashboardCycleContext(user, logs, stats, today, zone.location)
			if got := CalendarDayKey(context.DisplayNextPeriodStart); got != zone.key(0) {
				t.Fatalf("dashboard next period = %s, want the skipped day %s", got, zone.key(0))
			}
		})
		t.Run(zone.name+"/ovulation", func(t *testing.T) {
			user, logs := zone.owner(ovulationOnSkippedDayStart)
			today := DateAtLocation(zone.at(ovulationOnSkippedDayStart+5), zone.location)
			stats := BuildCycleStatsFromLogs(user, logs, today, zone.location)
			context := BuildDashboardCycleContext(user, logs, stats, today, zone.location)
			if got := CalendarDayKey(context.DisplayOvulationDate); got != zone.key(0) {
				t.Fatalf("dashboard ovulation = %s, want the skipped day %s", got, zone.key(0))
			}
		})
	}
}
