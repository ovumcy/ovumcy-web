package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/testenv"
)

// TestAveragePeriodLengthLeavesOutARunningPeriodUntilItEnds pins WEB-288: the
// running cycle's period is as long as the owner has logged it so far — one day
// on the day it is marked, and stopped at today by the onboarding fill — so it
// stays out of AveragePeriodLength until it has ended. Ended means a non-period
// day is logged after its last period day, or the owner's local today is past
// start + configured period length - 1.
//
// The configured period length is 6, so the running period that starts on
// 2026-02-26 runs through 2026-03-03 and is counted from 2026-03-04. The
// completed periods are 4 days (from 2026-01-01) and 6 days (from 2026-01-29):
// their average is 5, and every row that counts the running period moves it.
func TestAveragePeriodLengthLeavesOutARunningPeriodUntilItEnds(t *testing.T) {
	auckland := testenv.RequireTimeZone(t, "Pacific/Auckland")
	losAngeles := testenv.RequireTimeZone(t, "America/Los_Angeles")

	completed := func() []models.DailyLog {
		logs := periodRunLogs(t, "2026-01-01", 4)
		return append(logs, periodRunLogs(t, "2026-01-29", 6)...)
	}

	cases := []struct {
		name     string
		logs     []models.DailyLog
		now      time.Time
		location *time.Location
		want     float64
	}{
		{
			name: "day one marked today stays out",
			logs: append(completed(), periodRunLogs(t, "2026-02-26", 1)...),
			now:  utcNoon(t, "2026-02-26"),
			want: 5,
		},
		{
			name: "a run stopped at today stays out on its last configured day",
			logs: append(completed(), periodRunLogs(t, "2026-02-26", 2)...),
			now:  utcNoon(t, "2026-03-03"),
			want: 5,
		},
		{
			name: "today past start plus configured length minus one counts it",
			logs: append(completed(), periodRunLogs(t, "2026-02-26", 1)...),
			now:  utcNoon(t, "2026-03-04"),
			want: 11.0 / 3,
		},
		{
			name: "a non-period day logged after the last period day counts it",
			logs: append(append(completed(), periodRunLogs(t, "2026-02-26", 2)...),
				models.DailyLog{Date: mustParseDay(t, "2026-02-28")}),
			now:  utcNoon(t, "2026-02-28"),
			want: 4,
		},
		{
			name: "a non-period day logged before the run does not end it",
			logs: append(append(completed(), models.DailyLog{Date: mustParseDay(t, "2026-02-20")}),
				periodRunLogs(t, "2026-02-26", 2)...),
			now:  utcNoon(t, "2026-02-27"),
			want: 5,
		},
		{
			name: "a short completed period is still averaged",
			logs: append(append(periodRunLogs(t, "2026-01-01", 4), periodRunLogs(t, "2026-01-29", 1)...),
				periodRunLogs(t, "2026-02-26", 1)...),
			now:  utcNoon(t, "2026-02-26"),
			want: 2.5,
		},
		{
			name: "the running period alone falls back to the configured length",
			logs: periodRunLogs(t, "2026-02-26", 1),
			now:  utcNoon(t, "2026-02-26"),
			want: 6,
		},
		{
			// 2026-03-03T12:00Z is already 2026-03-04 in Auckland: the owner's day
			// is past the period, the UTC date is not.
			name:     "owner east of UTC is past the period before UTC is",
			logs:     append(completed(), periodRunLogs(t, "2026-02-26", 1)...),
			now:      time.Date(2026, time.March, 3, 12, 0, 0, 0, time.UTC),
			location: auckland,
			want:     11.0 / 3,
		},
		{
			// 2026-03-04T05:00Z is still 2026-03-03 in Los Angeles: the UTC date is
			// past the period, the owner's day is not.
			name:     "owner west of UTC is still inside the period after UTC is past it",
			logs:     append(completed(), periodRunLogs(t, "2026-02-26", 1)...),
			now:      time.Date(2026, time.March, 4, 5, 0, 0, 0, time.UTC),
			location: losAngeles,
			want:     5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 6}
			location := tc.location
			if location == nil {
				location = time.UTC
			}
			stats := BuildCycleStatsFromLogs(user, tc.logs, tc.now, location)
			if diff := stats.AveragePeriodLength - tc.want; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("AveragePeriodLength = %.4f, want %.4f", stats.AveragePeriodLength, tc.want)
			}
		})
	}
}

// TestBareCycleStatsKeepsTheRunningPeriod pins the other side of the rule: the
// stats built without an owner have no configured length to decide "ended"
// with, and average the running period exactly as before.
func TestBareCycleStatsKeepsTheRunningPeriod(t *testing.T) {
	logs := append(periodRunLogs(t, "2026-01-01", 4), periodRunLogs(t, "2026-01-29", 6)...)
	logs = append(logs, periodRunLogs(t, "2026-02-26", 1)...)

	stats := BuildCycleStats(logs, utcNoon(t, "2026-02-26"))
	if diff := stats.AveragePeriodLength - 11.0/3; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("AveragePeriodLength = %.4f, want %.4f", stats.AveragePeriodLength, 11.0/3)
	}
}

func periodRunLogs(t *testing.T, start string, days int) []models.DailyLog {
	t.Helper()
	first := mustParseDay(t, start)
	logs := make([]models.DailyLog, 0, days)
	for offset := 0; offset < days; offset++ {
		logs = append(logs, models.DailyLog{
			Date:       first.AddDate(0, 0, offset),
			IsPeriod:   true,
			CycleStart: offset == 0,
		})
	}
	return logs
}

func utcNoon(t *testing.T, day string) time.Time {
	t.Helper()
	return mustParseDay(t, day).Add(12 * time.Hour)
}
