package services

// calendar_feed_confirmed_ovulation_test.go — the .ics feed publishes the
// current cycle's temperature-confirmed ovulation day exactly when the
// dashboard, the calendar grid and the JSON API name it, while the webhook
// reminder stays prediction-only and future-only.
//
// Every cohort below is built from LOGS, not from a hand-written CycleStats:
// the feed derives its own stats from the logs it is handed, so a comparison
// against surfaces fed a hand-written struct would compare two inputs rather
// than two surfaces.

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const feedConfirmedDisclaimer = "Predictions are estimates, not medical advice or a method of contraception."

func renderConfirmedFeed(user *models.User, logs []models.DailyLog, now time.Time) string {
	return string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       logs,
		Now:        now,
		Location:   time.UTC,
		Disclaimer: feedConfirmedDisclaimer,
	}))
}

// feedEventUIDs returns every VEVENT UID in the body, in order.
func feedEventUIDs(body string) []string {
	var uids []string
	for _, line := range strings.Split(body, "\r\n") {
		if uid, ok := strings.CutPrefix(line, "UID:"); ok {
			uids = append(uids, uid)
		}
	}
	return uids
}

func countUID(uids []string, want string) int {
	count := 0
	for _, uid := range uids {
		if uid == want {
			count++
		}
	}
	return count
}

// TestCalendarFeedPublishesTheConfirmedDayOnceTheCycleIsOverdue is the
// overdue-only cohort: every projection is withheld, the confirmed day is
// the one event left, and the control without temperatures stays empty.
func TestCalendarFeedPublishesTheConfirmedDayOnceTheCycleIsOverdue(t *testing.T) {
	user, logs, _ := outboundConfirmedFixture(t, true)
	now := mustParseDashboardDay(t, "2026-04-10")

	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if !DashboardCycleOverdue(user, stats) {
		t.Fatalf("fixture: cycle day %d must be overdue", stats.CurrentCycleDay)
	}

	body := renderConfirmedFeed(user, logs, now)
	uids := feedEventUIDs(body)
	if len(uids) != 1 || uids[0] != "ovulation-20260311@ovumcy" {
		t.Fatalf("overdue feed UIDs = %v, want exactly [ovulation-20260311@ovumcy]:\n%s", uids, body)
	}
	for _, want := range []string{
		"DTSTART;VALUE=DATE:20260311",
		"DTEND;VALUE=DATE:20260312",
		"SUMMARY:Ovumcy: reminder (estimate)",
		// The prefix only: the full line is folded at 75 octets.
		"DESCRIPTION:Predictions are estimates",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("confirmed event lacks %q:\n%s", want, body)
		}
	}
	assertNeutralSummaries(t, body)

	_, controlLogs, _ := outboundConfirmedFixture(t, false)
	if control := renderConfirmedFeed(user, controlLogs, now); strings.Contains(control, "BEGIN:VEVENT") {
		t.Fatalf("control: an overdue cycle with no shift must carry no event:\n%s", control)
	}
}

// TestCalendarFeedWithholdsTheConfirmedDayUnderItsOwnGate pins the signals
// ConfirmedOvulationWithheld keeps: unpredictable-cycle mode, a pregnancy
// pause and the first-cycle floor each withhold the day from the feed as they
// do from the dashboard.
func TestCalendarFeedWithholdsTheConfirmedDayUnderItsOwnGate(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(user *models.User, logs []models.DailyLog) []models.DailyLog
	}{
		{name: "unpredictable cycle", mutate: func(user *models.User, logs []models.DailyLog) []models.DailyLog {
			user.UnpredictableCycle = true
			return logs
		}},
		{name: "pregnancy pause", mutate: func(_ *models.User, logs []models.DailyLog) []models.DailyLog {
			return append(logs, models.DailyLog{UserID: 1, Date: mustParseDashboardDay(t, "2026-03-13"), PregnancyTest: models.PregnancyTestPositive})
		}},
		{name: "first-cycle floor", mutate: func(_ *models.User, logs []models.DailyLog) []models.DailyLog {
			current := make([]models.DailyLog, 0, len(logs))
			for _, log := range logs {
				if !log.Date.Before(mustParseDashboardDay(t, "2026-03-01")) {
					current = append(current, log)
				}
			}
			return current
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			user, logs, now := outboundConfirmedFixture(t, true)
			logs = testCase.mutate(user, logs)

			stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
			if !ConfirmedOvulationWithheld(user, stats) {
				t.Fatalf("fixture: %s must withhold the confirmed day", testCase.name)
			}
			body := renderConfirmedFeed(user, logs, now)
			for _, uid := range feedEventUIDs(body) {
				if strings.HasPrefix(uid, calendarFeedKindOvulation+"-") {
					t.Fatalf("%s: the feed carries an ovulation event %s:\n%s", testCase.name, uid, body)
				}
			}
			if strings.Contains(body, "DTSTART;VALUE=DATE:20260311") {
				t.Fatalf("%s: the feed names the confirmed day:\n%s", testCase.name, body)
			}
		})
	}
}

// TestCalendarFeedConfirmedDayKeepsTheProjectionsUID is the in-place update: a
// shift confirmed on the very day the feed had projected carries the UID the
// projection carried, so a subscribed client updates that event rather than
// adding a second one.
func TestCalendarFeedConfirmedDayKeepsTheProjectionsUID(t *testing.T) {
	user, cycles, _ := outboundConfirmedFixture(t, false)
	lows := []string{"2026-03-09", "2026-03-10", "2026-03-11", "2026-03-12", "2026-03-13", "2026-03-14"}
	highs := []string{"2026-03-15", "2026-03-16", "2026-03-17"}

	before := append([]models.DailyLog{}, cycles...)
	for _, day := range lows {
		before = append(before, outboundBBTLog(t, user.ID, day, 36.20))
	}
	beforeUIDs := feedEventUIDs(renderConfirmedFeed(user, before, mustParseDashboardDay(t, "2026-03-14")))
	const want = "ovulation-20260314@ovumcy"
	if countUID(beforeUIDs, want) != 1 {
		t.Fatalf("fixture: the pre-shift feed must project %s once, got %v", want, beforeUIDs)
	}

	after := append([]models.DailyLog{}, before...)
	for _, day := range highs {
		after = append(after, outboundBBTLog(t, user.ID, day, 36.50))
	}
	now := mustParseDashboardDay(t, "2026-03-17")
	stats := BuildCycleStatsFromLogs(user, after, now, time.UTC)
	confirmed, ok := ConfirmedCurrentCycleOvulation(user, after, stats, now, time.UTC)
	if !ok || CalendarDayKey(confirmed) != "2026-03-14" {
		t.Fatalf("fixture: confirmed = %s (ok=%t), want 2026-03-14", CalendarDayKey(confirmed), ok)
	}

	body := renderConfirmedFeed(user, after, now)
	afterUIDs := feedEventUIDs(body)
	if got := countUID(afterUIDs, want); got != 1 {
		t.Fatalf("post-shift feed carries %s %d time(s), want exactly once: %v\n%s", want, got, afterUIDs, body)
	}
	if got := strings.Count(body, "DTSTART;VALUE=DATE:20260314"); got != 1 {
		t.Fatalf("post-shift feed names 2026-03-14 %d time(s), want once:\n%s", got, body)
	}
}

// feedSurfaceCohort is one log-built account and the day every surface is
// asked on.
type feedSurfaceCohort struct {
	name        string
	build       func(t *testing.T) (*models.User, []models.DailyLog, time.Time)
	confirmed   string
	wantOverdue bool
	check       func(t *testing.T, stats CycleStats, today time.Time)
}

func feedSurfaceCohorts() []feedSurfaceCohort {
	return []feedSurfaceCohort{
		{
			name: "overdue only",
			build: func(t *testing.T) (*models.User, []models.DailyLog, time.Time) {
				user, logs, _ := outboundConfirmedFixture(t, true)
				return user, logs, mustParseDashboardDay(t, "2026-04-10")
			},
			confirmed:   "2026-03-11",
			wantOverdue: true,
		},
		{
			name: "today past NextPeriodStart, not overdue",
			build: func(t *testing.T) (*models.User, []models.DailyLog, time.Time) {
				user, logs, _ := outboundConfirmedFixture(t, true)
				return user, logs, mustParseDashboardDay(t, "2026-03-31")
			},
			confirmed: "2026-03-11",
			check: func(t *testing.T, stats CycleStats, today time.Time) {
				if stats.NextPeriodStart.IsZero() || today.Before(CalendarDay(stats.NextPeriodStart, time.UTC)) {
					t.Fatalf("fixture: today %s must be on or after NextPeriodStart %s", CalendarDayKey(today), CalendarDayKey(stats.NextPeriodStart))
				}
			},
		},
		{
			name:      "NextPeriodStart set, OvulationDate empty, OvulationImpossible",
			build:     impossibleOvulationFeedFixture,
			confirmed: "2026-03-11",
			check: func(t *testing.T, stats CycleStats, today time.Time) {
				if stats.NextPeriodStart.IsZero() || !stats.OvulationDate.IsZero() || !stats.OvulationImpossible {
					t.Fatalf("fixture: next=%s ovulation=%s impossible=%t, want a next period, no ovulation date and OvulationImpossible",
						CalendarDayKey(stats.NextPeriodStart), CalendarDayKey(stats.OvulationDate), stats.OvulationImpossible)
				}
				if today.Before(CalendarDay(stats.NextPeriodStart, time.UTC)) {
					t.Fatalf("fixture: today %s must be on or after NextPeriodStart %s", CalendarDayKey(today), CalendarDayKey(stats.NextPeriodStart))
				}
			},
		},
	}
}

// impossibleOvulationFeedFixture is a 14-day cadence — one day short of the
// shortest cycle CalcOvulationDay can seat an ovulation in — with a thermal
// shift confirmed on 2026-03-11 and today on the projected next start.
func impossibleOvulationFeedFixture(t *testing.T) (*models.User, []models.DailyLog, time.Time) {
	t.Helper()
	user := &models.User{ID: 1, Role: models.RoleOwner, TrackBBT: true, CycleLength: 14, PeriodLength: 3}
	logs := make([]models.DailyLog, 0, 24)
	for _, start := range []string{"2026-01-19", "2026-02-02", "2026-02-16", "2026-03-02"} {
		cycleStart := mustParseDashboardDay(t, start)
		for offset := range 3 {
			logs = append(logs, models.DailyLog{
				UserID:     user.ID,
				Date:       cycleStart.AddDate(0, 0, offset),
				IsPeriod:   true,
				CycleStart: offset == 0,
				Flow:       models.FlowMedium,
			})
		}
	}
	for _, day := range []string{"2026-03-06", "2026-03-07", "2026-03-08", "2026-03-09", "2026-03-10", "2026-03-11"} {
		logs = append(logs, outboundBBTLog(t, user.ID, day, 36.20))
	}
	for _, day := range []string{"2026-03-12", "2026-03-13", "2026-03-14"} {
		logs = append(logs, outboundBBTLog(t, user.ID, day, 36.50))
	}
	return user, logs, mustParseDashboardDay(t, "2026-03-16")
}

// TestEverySurfaceAgreesOnTheConfirmedDayIncludingTheFeed asks the JSON API,
// the dashboard, the calendar grid, the .ics feed and the webhook pass about
// one log-built account per cohort. The four owner-readable surfaces name the
// confirmed day; the webhook never does, because it announces days still
// ahead.
func TestEverySurfaceAgreesOnTheConfirmedDayIncludingTheFeed(t *testing.T) {
	for _, cohort := range feedSurfaceCohorts() {
		t.Run(cohort.name, func(t *testing.T) {
			user, logs, today := cohort.build(t)
			stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
			if got := DashboardCycleOverdue(user, stats); got != cohort.wantOverdue {
				t.Fatalf("fixture: overdue = %t at cycle day %d, want %t", got, stats.CurrentCycleDay, cohort.wantOverdue)
			}
			if cohort.check != nil {
				cohort.check(t, stats, today)
			}
			confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC)
			if !ok || CalendarDayKey(confirmed) != cohort.confirmed {
				t.Fatalf("fixture: confirmed = %s (ok=%t), want %s", CalendarDayKey(confirmed), ok, cohort.confirmed)
			}

			published, _, apiConfirmed := PublishedOverviewStats(user, logs, stats, today, time.UTC)
			if !apiConfirmed || CalendarDayKey(published.OvulationDate) != cohort.confirmed {
				t.Errorf("API: ovulation %s (confirmed=%t), want %s", CalendarDayKey(published.OvulationDate), apiConfirmed, cohort.confirmed)
			}

			cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
			if !cycleContext.DisplayOvulationConfirmed || CalendarDayKey(cycleContext.DisplayOvulationDate) != cohort.confirmed {
				t.Errorf("dashboard: ovulation %s (confirmed=%t), want %s", CalendarDayKey(cycleContext.DisplayOvulationDate), cycleContext.DisplayOvulationConfirmed, cohort.confirmed)
			}

			monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
			solid, _ := ovulationMarkerKeys(BuildCalendarDayStates(user, monthStart, logs, stats, today, time.UTC))
			if len(solid) != 1 || solid[0] != cohort.confirmed {
				t.Errorf("calendar: solid ovulation markers %v, want [%s]", solid, cohort.confirmed)
			}

			confirmedStamp := strings.ReplaceAll(cohort.confirmed, "-", "")
			uids := feedEventUIDs(renderConfirmedFeed(user, logs, today))
			if countUID(uids, "ovulation-"+confirmedStamp+"@ovumcy") != 1 {
				t.Errorf(".ics: UIDs %v, want ovulation-%s@ovumcy exactly once", uids, confirmedStamp)
			}

			for _, reminder := range DecideDueReminders(user, enabledWebhookSettings(14), logs, today, time.UTC) {
				if reminder.EventDate.Before(today) || CalendarDayKey(reminder.EventDate) == cohort.confirmed {
					t.Errorf("webhook: reminder %s for %s, want future-only projections", reminder.Type, CalendarDayKey(reminder.EventDate))
				}
			}
		})
	}
}
