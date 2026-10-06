package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// manualTickFixture is an onboarded owner (start 2026-09-14, period length 5,
// auto-fill as given) whose days are written the way the composition root
// writes them, plus the handle to adjust what onboarding stored.
type manualTickFixture struct {
	database     *gorm.DB
	repositories *db.Repositories
	days         *DayService
	userID       uint
}

func newManualTickFixture(t *testing.T, email string, autoFill bool) manualTickFixture {
	t.Helper()
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	user := createDayServiceTestUser(t, database, email)
	if err := repositories.Users.UpdateByID(context.Background(), user.ID, map[string]any{"auto_period_fill": autoFill, "period_length": 5}); err != nil {
		t.Fatalf("set auto-fill: %v", err)
	}
	if err := repositories.Users.CompleteOnboarding(context.Background(), user.ID, startMoveDay(time.September, 14), startMoveDay(time.September, 18), autoFill); err != nil {
		t.Fatalf("complete onboarding: %v", err)
	}
	return manualTickFixture{database: database, repositories: repositories, days: NewDayService(repositories.DailyLogs, repositories.Users), userID: user.ID}
}

// tickByHand saves day as a bare period day through the day service, the way
// the day editor does. Each hand tick is its own request, made well after
// onboarding: the row's creation time is spread to say so, so a coarse clock
// cannot fold two requests into one instant.
func (fixture manualTickFixture) tickByHand(t *testing.T, day time.Time, hoursAfterOnboarding int) {
	t.Helper()
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	entry, err := fixture.days.UpsertDayEntryWithAutoFillAt(context.Background(), fixture.userID, day, DayEntryInput{IsPeriod: true, Flow: models.FlowNone}, now, time.UTC)
	if err != nil {
		t.Fatalf("tick %s: %v", day.Format("2006-01-02"), err)
	}
	if !IsAutoFilledPeriodCandidate(entry, "") {
		t.Fatalf("fixture: the hand tick on %s is not bare, so it cannot tell the fill apart: %+v", day.Format("2006-01-02"), entry)
	}
	stamp := time.Now().UTC().Add(time.Duration(hoursAfterOnboarding) * time.Hour)
	if err := fixture.database.Model(&models.DailyLog{}).Where("id = ? AND user_id = ?", entry.ID, fixture.userID).Update("created_at", stamp).Error; err != nil {
		t.Fatalf("stamp %s: %v", day.Format("2006-01-02"), err)
	}
}

func (fixture manualTickFixture) moveStart(t *testing.T, newStart time.Time) []models.DailyLog {
	t.Helper()
	settings := NewSettingsService(fixture.repositories.Users)
	settings.AttachDayLogReader(fixture.days)
	if err := settings.SaveCycleSettings(context.Background(), fixture.userID, CycleSettingsUpdate{LastPeriodStartSet: true, LastPeriodStart: &newStart}); err != nil {
		t.Fatalf("SaveCycleSettings: %v", err)
	}
	logs, err := fixture.repositories.DailyLogs.ListByUser(context.Background(), fixture.userID)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	return logs
}

func assertPeriodDaysKept(t *testing.T, logs []models.DailyLog, from time.Time, count int) {
	t.Helper()
	for offset := range count {
		day := from.AddDate(0, 0, offset).Format("2006-01-02")
		if !startMoveHasPeriodDay(logs, day) {
			t.Errorf("the period day ticked by hand on %s was deleted by the start move (logged %v)", day, startMoveLoggedDays(logs))
		}
	}
}

// TestSettingsStartMoveKeepsHandTicksMadeWithAutoFillOff: onboarding with
// auto-fill off wrote no days, so every bare period day after it is the owner's
// own tick. Turning auto-fill on later and moving the start two days earlier
// must not take any of them for onboarding's fill.
func TestSettingsStartMoveKeepsHandTicksMadeWithAutoFillOff(t *testing.T) {
	fixture := newManualTickFixture(t, "start-move-hand-ticks-off@example.com", false)
	start := startMoveDay(time.September, 14)
	for offset := range 5 {
		fixture.tickByHand(t, start.AddDate(0, 0, offset), offset+1)
	}
	if err := fixture.repositories.Users.UpdateByID(context.Background(), fixture.userID, map[string]any{"auto_period_fill": true}); err != nil {
		t.Fatalf("turn auto-fill on: %v", err)
	}

	logs := fixture.moveStart(t, start.AddDate(0, 0, -2))
	assertPeriodDaysKept(t, logs, start, 5)
}

// TestSettingsStartMoveKeepsHandTicksPastALengthChangedSinceOnboarding:
// onboarding filled five days; the owner ticked the next two by hand and then
// set the period length to seven. Moving the start earlier must not read the
// new length back into what onboarding wrote.
func TestSettingsStartMoveKeepsHandTicksPastALengthChangedSinceOnboarding(t *testing.T) {
	fixture := newManualTickFixture(t, "start-move-hand-ticks-length@example.com", true)
	start := startMoveDay(time.September, 14)
	fixture.tickByHand(t, start.AddDate(0, 0, 5), 1)
	fixture.tickByHand(t, start.AddDate(0, 0, 6), 2)
	if err := fixture.repositories.Users.UpdateByID(context.Background(), fixture.userID, map[string]any{"period_length": 7}); err != nil {
		t.Fatalf("set period length 7: %v", err)
	}

	logs := fixture.moveStart(t, start.AddDate(0, 0, -2))
	assertPeriodDaysKept(t, logs, start.AddDate(0, 0, 5), 2)
}
