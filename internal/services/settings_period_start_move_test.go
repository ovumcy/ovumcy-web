package services

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The production repository carries out the move; without this the service
// would refuse every Settings start change.
var _ periodStartMover = (*db.UserRepository)(nil)

func startMoveDay(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC)
}

// startMoveFixture onboards an owner on 2026-10-03 with auto-fill (period
// length 5), lets touch edit the stored days, then moves the start to
// 2026-09-06 in Settings. It returns the reloaded owner and logs.
func startMoveFixture(t *testing.T, email string, touch func(*testing.T, *db.Repositories, uint)) (models.User, []models.DailyLog) {
	t.Helper()
	return startMoveFixtureAt(t, email, startMoveDay(time.October, 3), startMoveDay(time.September, 6), touch)
}

// startMoveFixtureAt is startMoveFixture with the onboarding and the new start
// chosen by the caller. The settings service reads the logs through the day
// service, as bootstrap wires it.
func startMoveFixtureAt(t *testing.T, email string, onboarded time.Time, moved time.Time, touch func(*testing.T, *db.Repositories, uint)) (models.User, []models.DailyLog) {
	t.Helper()
	ctx := context.Background()
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	user := createDayServiceTestUser(t, database, email)
	if err := repositories.Users.UpdateByID(ctx, user.ID, map[string]any{"auto_period_fill": true}); err != nil {
		t.Fatalf("enable auto-fill: %v", err)
	}
	if err := repositories.Users.CompleteOnboarding(ctx, user.ID, onboarded, onboarded.AddDate(0, 0, 4), true); err != nil {
		t.Fatalf("complete onboarding: %v", err)
	}
	if touch != nil {
		touch(t, repositories, user.ID)
	}

	update := CycleSettingsUpdate{LastPeriodStartSet: true, LastPeriodStart: &moved}
	settings := NewSettingsService(repositories.Users)
	settings.AttachDayLogReader(NewDayService(repositories.DailyLogs, repositories.Users))
	if err := settings.SaveCycleSettings(ctx, user.ID, update); err != nil {
		t.Fatalf("SaveCycleSettings: %v", err)
	}

	stored, err := repositories.Users.LoadSettingsByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	stored.Role = models.RoleOwner
	logs, err := repositories.DailyLogs.ListByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	return stored, logs
}

func startMoveLoggedDays(logs []models.DailyLog) []string {
	days := make([]string, 0, len(logs))
	for _, logEntry := range logs {
		days = append(days, logEntry.Date.UTC().Format("2006-01-02"))
	}
	sort.Strings(days)
	return days
}

// TestSettingsStartMoveLeavesNoPhantomCycle: onboarding on 10-03 with auto-fill
// wrote 10-03..10-07; moving the start to 09-06 must take those days along, or
// they form a boundary of their own — a phantom cycle — and keep the dashboard
// anchored on 10-03.
func TestSettingsStartMoveLeavesNoPhantomCycle(t *testing.T) {
	stored, logs := startMoveFixture(t, "start-move@example.com", nil)
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)

	if stored.LastPeriodStart == nil || !stored.LastPeriodStart.Equal(startMoveDay(time.September, 6)) {
		t.Fatalf("stored start = %v, want 2026-09-06", stored.LastPeriodStart)
	}
	stats := BuildCycleStatsFromLogs(&stored, logs, now, time.UTC)
	if got := stats.LastPeriodStart.Format("2006-01-02"); got != "2026-09-06" || stats.CompletedCycleCount != 0 {
		t.Fatalf("anchor=%s completed=%d logged=%v, want anchor 2026-09-06 and no completed cycle", got, stats.CompletedCycleCount, startMoveLoggedDays(logs))
	}
	assertBoundaries(t, logs, BoundaryContextFor(&stored, DateAtLocation(now, time.UTC)), "2026-09-06")
	want := []string{"2026-09-06", "2026-09-07", "2026-09-08", "2026-09-09", "2026-09-10"}
	if got := startMoveLoggedDays(logs); len(got) != len(want) || got[0] != want[0] || got[len(got)-1] != want[len(want)-1] {
		t.Fatalf("logged days = %v, want the fill moved to %v", got, want)
	}
}

// TestSettingsStartMoveKeepsADayTheOwnerTouched: a fill day the owner edited is
// the owner's data, not the fill's, and survives the move. The clear walks from
// the old start and stops there, as un-ticking an auto-filled period does: the
// fill days before it go, the days after it stay. The touched day is the third
// of the fill: the two before it are the cohort the move needs to see before it
// clears anything (oldStartFillRun).
func TestSettingsStartMoveKeepsADayTheOwnerTouched(t *testing.T) {
	touched := startMoveDay(time.October, 5)
	_, logs := startMoveFixture(t, "start-move-touched@example.com", func(t *testing.T, repositories *db.Repositories, userID uint) {
		t.Helper()
		entry, found, err := repositories.DailyLogs.FindByUserAndDayRange(context.Background(), userID, touched, touched.AddDate(0, 0, 1))
		if err != nil || !found {
			t.Fatalf("fixture: onboarding wrote no 10-05 row (found=%v err=%v)", found, err)
		}
		entry.Mood = 3
		if err := repositories.DailyLogs.Save(context.Background(), &entry); err != nil {
			t.Fatalf("touch 10-05: %v", err)
		}
	})

	var kept *models.DailyLog
	for index := range logs {
		day := logs[index].Date.UTC().Format("2006-01-02")
		switch day {
		case "2026-10-05":
			kept = &logs[index]
		case "2026-10-03", "2026-10-04":
			t.Errorf("untouched fill day %s, before the touched one, survived the move", day)
		}
	}
	for _, day := range []string{"2026-10-06", "2026-10-07"} {
		if !startMoveHasPeriodDay(logs, day) {
			t.Errorf("fill day %s past the touched one was deleted: the clear must stop at the first day the owner edited", day)
		}
	}
	if kept == nil || kept.Mood != 3 || !kept.IsPeriod {
		t.Fatalf("the touched day did not survive as the owner left it: %+v (logged %v)", kept, startMoveLoggedDays(logs))
	}
}

func startMoveHasPeriodDay(logs []models.DailyLog, day string) bool {
	for _, logEntry := range logs {
		if logEntry.Date.UTC().Format("2006-01-02") == day && logEntry.IsPeriod {
			return true
		}
	}
	return false
}

// TestSettingsStartMoveAShortestCycleLaterKeepsTheOldDays: a start moved later
// by at least the shortest cycle onboarding accepts is a new period, not a
// correction of the old one, so the old start's fill days stay recorded. One
// day short of that bound is still a correction and takes them along.
func TestSettingsStartMoveAShortestCycleLaterKeepsTheOldDays(t *testing.T) {
	onboarded := startMoveDay(time.September, 6)
	later := onboarded.AddDate(0, 0, MinOnboardingCycleLength)
	stored, logs := startMoveFixtureAt(t, "start-move-later@example.com", onboarded, later, nil)
	if stored.LastPeriodStart == nil || !stored.LastPeriodStart.Equal(later) {
		t.Fatalf("stored start = %v, want %s", stored.LastPeriodStart, later.Format("2006-01-02"))
	}
	for _, day := range []string{"2026-09-06", "2026-09-10", "2026-09-21"} {
		if !startMoveHasPeriodDay(logs, day) {
			t.Fatalf("period day %s missing after a move a shortest cycle later: logged %v", day, startMoveLoggedDays(logs))
		}
	}

	_, logs = startMoveFixtureAt(t, "start-move-later-short@example.com", onboarded, later.AddDate(0, 0, -1), nil)
	if startMoveHasPeriodDay(logs, "2026-09-06") {
		t.Fatalf("a move one day short of the bound kept the old start's fill: logged %v", startMoveLoggedDays(logs))
	}
}

// TestSettingsStartMoveKeepsTheOldDaysOnceALaterCycleIsLogged: once the owner
// logged a cycle after the old start, the old start no longer opens the newest
// cycle; moving it rewrites history the later cycle is measured from, so its
// fill days stay.
func TestSettingsStartMoveKeepsTheOldDaysOnceALaterCycleIsLogged(t *testing.T) {
	onboarded := startMoveDay(time.September, 6)
	_, logs := startMoveFixtureAt(t, "start-move-later-cycle@example.com", onboarded, startMoveDay(time.September, 1), func(t *testing.T, repositories *db.Repositories, userID uint) {
		t.Helper()
		for _, day := range []time.Time{startMoveDay(time.October, 1), startMoveDay(time.October, 2)} {
			entry := models.DailyLog{UserID: userID, Date: day, IsPeriod: true, Flow: models.FlowMedium}
			if err := repositories.DailyLogs.Create(context.Background(), &entry); err != nil {
				t.Fatalf("log %s: %v", day.Format("2006-01-02"), err)
			}
		}
	})
	for _, day := range []string{"2026-09-01", "2026-09-06", "2026-09-10", "2026-10-01"} {
		if !startMoveHasPeriodDay(logs, day) {
			t.Fatalf("period day %s missing after the move: logged %v", day, startMoveLoggedDays(logs))
		}
	}
}
