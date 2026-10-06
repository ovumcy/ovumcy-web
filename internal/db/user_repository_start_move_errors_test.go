package db

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// The start move writes the settings columns and the day logs in one
// transaction. These tests fail each statement inside it in turn — a read by
// dropping the table, a write by a trigger that aborts it — and check that the
// error is returned and that the settings column written last was rolled back
// with everything before it.

const startMoveFailureCycleLength = 33

func openStartMoveFailureDatabase(t *testing.T, name string) (*gorm.DB, uint) {
	t.Helper()
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), name))
	owner := createDailyLogTestUser(t, database, strings.TrimSuffix(name, ".db")+"@example.com")
	return database, owner
}

func startMoveFailureDay() time.Time {
	return time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
}

func createStartMoveDay(t *testing.T, database *gorm.DB, owner uint, day time.Time, isPeriod bool) {
	t.Helper()
	entry := &models.DailyLog{UserID: owner, Date: day, IsPeriod: isPeriod, Flow: models.FlowNone}
	if err := NewDailyLogRepository(database).Create(context.Background(), entry); err != nil {
		t.Fatalf("create day log %s: %v", day.Format("2006-01-02"), err)
	}
}

func abortDayLogStatement(t *testing.T, database *gorm.DB, event string) string {
	t.Helper()
	message := "day log " + strings.ToLower(event) + " refused"
	if err := database.Exec(`CREATE TRIGGER refuse_day_log_` + strings.ToLower(event) + ` BEFORE ` + event +
		` ON daily_logs BEGIN SELECT RAISE(ABORT, '` + message + `'); END`).Error; err != nil {
		t.Fatalf("create the %s trigger: %v", event, err)
	}
	return message
}

func runStartMove(database *gorm.DB, owner uint, move models.PeriodStartMove) error {
	return NewUserRepository(database).UpdateCycleSettingsMovingPeriodStart(context.Background(), owner,
		map[string]any{"cycle_length": startMoveFailureCycleLength}, move)
}

func requireStartMoveColumnsRolledBack(t *testing.T, database *gorm.DB, owner uint) {
	t.Helper()
	var cycleLength int
	if err := database.Raw(`SELECT cycle_length FROM users WHERE id = ?`, owner).Scan(&cycleLength).Error; err != nil {
		t.Fatalf("read cycle_length: %v", err)
	}
	if cycleLength == startMoveFailureCycleLength {
		t.Fatalf("the settings columns must roll back with the failed day-log move, cycle_length=%d", cycleLength)
	}
}

func countStartMoveDays(t *testing.T, database *gorm.DB, owner uint, isPeriod bool) int64 {
	t.Helper()
	var count int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ? AND is_period = ?", owner, isPeriod).Count(&count).Error; err != nil {
		t.Fatalf("count day logs: %v", err)
	}
	return count
}

func TestStartMoveRefusesAZeroOwner(t *testing.T) {
	database, _ := openStartMoveFailureDatabase(t, "start-move-zero-owner.db")
	err := runStartMove(database, 0, models.PeriodStartMove{MarkDay: startMoveFailureDay()})
	if !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("expected ErrUserOwnerRequired, got %v", err)
	}
}

func TestStartMoveFailsWhenTheOldRangeCannotBeRead(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "start-move-read.db")
	if err := database.Exec(`DROP TABLE daily_logs`).Error; err != nil {
		t.Fatalf("drop the day logs: %v", err)
	}
	day := startMoveFailureDay()
	err := runStartMove(database, owner, models.PeriodStartMove{
		ClearFrom: day,
		ClearTo:   day.AddDate(0, 0, 5),
		ClearRows: func(entries []models.DailyLog) []models.DailyLog { return entries },
	})
	if err == nil || !strings.Contains(err.Error(), "daily_logs") {
		t.Fatalf("expected the day-log read to fail, got %v", err)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

func TestStartMoveFailsWhenAnOldFillRowCannotBeDeleted(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "start-move-delete.db")
	day := startMoveFailureDay()
	createStartMoveDay(t, database, owner, day, true)
	message := abortDayLogStatement(t, database, "DELETE")

	err := runStartMove(database, owner, models.PeriodStartMove{
		ClearFrom: day,
		ClearTo:   day.AddDate(0, 0, 1),
		ClearRows: func(entries []models.DailyLog) []models.DailyLog { return entries },
	})
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("expected the delete to fail with %q, got %v", message, err)
	}
	if got := countStartMoveDays(t, database, owner, true); got != 1 {
		t.Fatalf("the old fill row must survive a failed move, got %d period row(s)", got)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

func TestStartMoveFailsWhenAFillDayCannotBeChecked(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "start-move-count.db")
	if err := database.Exec(`DROP TABLE daily_logs`).Error; err != nil {
		t.Fatalf("drop the day logs: %v", err)
	}
	err := runStartMove(database, owner, models.PeriodStartMove{FillDays: []time.Time{startMoveFailureDay()}})
	if err == nil || !strings.Contains(err.Error(), "daily_logs") {
		t.Fatalf("expected the fill-day check to fail, got %v", err)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

func TestStartMoveFailsWhenAFillDayCannotBeWritten(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "start-move-create.db")
	message := abortDayLogStatement(t, database, "INSERT")

	day := startMoveFailureDay()
	err := runStartMove(database, owner, models.PeriodStartMove{FillDays: []time.Time{day, day.AddDate(0, 0, 1)}})
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("expected the fill write to fail with %q, got %v", message, err)
	}
	if got := countStartMoveDays(t, database, owner, true); got != 0 {
		t.Fatalf("a failed move must write no fill day, got %d", got)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

func TestStartMoveFailsWhenTheNewStartCannotBeMarked(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "start-move-mark.db")
	day := startMoveFailureDay()
	createStartMoveDay(t, database, owner, day, false)
	message := abortDayLogStatement(t, database, "UPDATE")

	err := runStartMove(database, owner, models.PeriodStartMove{MarkDay: day})
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("expected the mark to fail with %q, got %v", message, err)
	}
	if got := countStartMoveDays(t, database, owner, false); got != 1 {
		t.Fatalf("the new start day must stay unmarked after a failed move, got %d non-period row(s)", got)
	}
	requireStartMoveColumnsRolledBack(t, database, owner)
}

// TestClearLastPeriodStartOnRefusesAZeroOwner pins that a zero owner id is an
// error, never a reason to drop the user scope: no account's stored start is
// cleared by it.
func TestClearLastPeriodStartOnRefusesAZeroOwner(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "clear-start-zero-owner.db")
	day := startMoveFailureDay()
	if err := database.Model(&models.User{}).Where("id = ?", owner).Update("last_period_start", day).Error; err != nil {
		t.Fatalf("store the owner's start: %v", err)
	}

	if err := NewDailyLogRepository(database).ClearLastPeriodStartOn(context.Background(), 0, day); err == nil {
		t.Fatal("expected a zero owner id to be refused")
	}
	var stored int64
	if err := database.Model(&models.User{}).Where("id = ? AND last_period_start IS NOT NULL", owner).Count(&stored).Error; err != nil {
		t.Fatalf("read the owner's start: %v", err)
	}
	if stored != 1 {
		t.Fatal("a refused clear must leave every stored start in place")
	}
}
