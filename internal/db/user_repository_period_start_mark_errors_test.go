package db

import (
	"context"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The cycle-start mark's own statements run inside the start move's
// transaction. Each one is failed in turn and the error must come back with
// the settings column and the day rows exactly as they were.

func TestPeriodStartMoveFailsWhenTheReleasedStartCannotBeRead(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "period-start-release-read.db")
	if err := database.Exec(`DROP TABLE daily_logs`).Error; err != nil {
		t.Fatalf("drop the day logs: %v", err)
	}
	err := runStartMove(database, owner, models.PeriodStartMove{ReleaseStart: startMoveFailureDay()})
	if err == nil || !strings.Contains(err.Error(), "daily_logs") {
		t.Fatalf("expected the released start's read to fail, got %v", err)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

func TestPeriodStartMoveFailsWhenABareReleasedStartCannotBeDeleted(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "period-start-release-delete.db")
	day := startMoveFailureDay()
	if err := database.Create(&models.DailyLog{UserID: owner, Date: day, IsPeriod: true, CycleStart: true, Flow: models.FlowNone}).Error; err != nil {
		t.Fatalf("create the marked start: %v", err)
	}
	message := abortDayLogStatement(t, database, "DELETE")

	err := runStartMove(database, owner, models.PeriodStartMove{
		ReleaseStart:  day,
		ReleaseIsBare: func(models.DailyLog) bool { return true },
	})
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("expected the delete to fail with %q, got %v", message, err)
	}
	var marked int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ? AND cycle_start = ?", owner, true).Count(&marked).Error; err != nil {
		t.Fatalf("count marked rows: %v", err)
	}
	if marked != 1 {
		t.Fatalf("the marked start must survive a failed move, got %d", marked)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

func TestPeriodStartMoveFailsWhenAReleasedStartCannotLoseItsMark(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "period-start-release-update.db")
	day := startMoveFailureDay()
	if err := database.Create(&models.DailyLog{UserID: owner, Date: day, IsPeriod: true, CycleStart: true, Flow: models.FlowLight}).Error; err != nil {
		t.Fatalf("create the marked start: %v", err)
	}
	message := abortDayLogStatement(t, database, "UPDATE")

	err := runStartMove(database, owner, models.PeriodStartMove{ReleaseStart: day})
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("expected the unmark to fail with %q, got %v", message, err)
	}
	var marked int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ? AND cycle_start = ?", owner, true).Count(&marked).Error; err != nil {
		t.Fatalf("count marked rows: %v", err)
	}
	if marked != 1 {
		t.Fatalf("the mark must stay after a failed move, got %d marked row(s)", marked)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

func TestPeriodStartMoveFailsWhenTheNewStartDayCannotBeChecked(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "period-start-mark-count.db")
	if err := database.Exec(`DROP TABLE daily_logs`).Error; err != nil {
		t.Fatalf("drop the day logs: %v", err)
	}
	err := runStartMove(database, owner, models.PeriodStartMove{MarkDay: startMoveFailureDay()})
	if err == nil || !strings.Contains(err.Error(), "daily_logs") {
		t.Fatalf("expected the new start day's check to fail, got %v", err)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

func TestPeriodStartMoveFailsWhenTheNewStartDayCannotBecomeAPeriodDay(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "period-start-mark-period.db")
	day := startMoveFailureDay()
	createStartMoveDay(t, database, owner, day, false)
	message := abortDayLogStatement(t, database, "UPDATE")

	err := runStartMove(database, owner, models.PeriodStartMove{MarkDay: day})
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("expected the period write to fail with %q, got %v", message, err)
	}
	if got := countStartMoveDays(t, database, owner, true); got != 0 {
		t.Fatalf("a failed move must not turn the day into a period day, got %d", got)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

func TestPeriodStartMoveFailsWhenTheNewStartDayCannotBeMarked(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "period-start-mark-flag.db")
	day := startMoveFailureDay()
	createStartMoveDay(t, database, owner, day, true)
	message := abortDayLogStatement(t, database, "UPDATE")

	err := runStartMove(database, owner, models.PeriodStartMove{MarkDay: day})
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("expected the mark write to fail with %q, got %v", message, err)
	}
	var marked int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ? AND cycle_start = ?", owner, true).Count(&marked).Error; err != nil {
		t.Fatalf("count marked rows: %v", err)
	}
	if marked != 0 {
		t.Fatalf("a failed move must leave the day unmarked, got %d", marked)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

// A zero owner id is an error, never a reason to drop the user scope: no
// account's stored start moves.
func TestMoveLastPeriodStartOnRefusesAZeroOwner(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "move-start-zero-owner.db")
	from := startMoveFailureDay()
	if err := database.Model(&models.User{}).Where("id = ?", owner).Update("last_period_start", from).Error; err != nil {
		t.Fatalf("store the owner's start: %v", err)
	}

	err := NewDailyLogRepository(database).MoveLastPeriodStartOn(context.Background(), 0, from, from.AddDate(0, 0, 3))
	if err == nil {
		t.Fatal("expected a zero owner id to be refused")
	}
	var unmoved int64
	if err := database.Model(&models.User{}).Where("id = ? AND last_period_start >= ? AND last_period_start < ?", owner, from, from.AddDate(0, 0, 1)).Count(&unmoved).Error; err != nil {
		t.Fatalf("read the owner's start: %v", err)
	}
	if unmoved != 1 {
		t.Fatal("a refused move must leave every stored start in place")
	}
}
