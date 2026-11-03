package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Replacing a cycle start demotes the competing starts of its cluster and, when
// the demoted one is the stored start, moves the stored start with it. Each
// failure on that path comes back and leaves the stored start where it was.

type movingStartDayLogs struct {
	*dayLogRepositoryStub
	moveErr error
	moves   int
}

func (stub *movingStartDayLogs) MoveLastPeriodStartOn(_ context.Context, _ uint, _ time.Time, _ time.Time) error {
	stub.moves++
	return stub.moveErr
}

// competingStartNow puts the fixtures' March days in the past, so a replace of
// the stored start moves it.
var competingStartNow = time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC)

func competingStartFixture() (earlier models.DailyLog, selected models.DailyLog, all []models.DailyLog) {
	earlierDay := time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)
	targetDay := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	earlier = models.DailyLog{ID: 1, UserID: 10, Date: earlierDay, IsPeriod: true, CycleStart: true}
	selected = models.DailyLog{ID: 2, UserID: 10, Date: targetDay, IsPeriod: true, CycleStart: true}
	return earlier, selected, []models.DailyLog{earlier, selected}
}

func TestCompetingCycleStartReplaceFailsWhenTheStoredStartCannotBeRead(t *testing.T) {
	earlier, selected, all := competingStartFixture()
	logs := newDayLogRepositoryStub()
	logs.entries["2026-03-08"] = earlier
	logs.entries["2026-03-10"] = selected
	injected := errors.New("driver: database is locked")
	service := NewDayService(logs, &dayUserRepositoryStub{loadErr: injected})

	err := service.clearCompetingCycleStarts(context.Background(), 10, all, selected, competingStartNow, time.UTC)
	if !errors.Is(err, injected) {
		t.Fatalf("expected the failed stored-start read back, got %v", err)
	}
	if !logs.entries["2026-03-08"].CycleStart {
		t.Fatal("a refused replace must leave the competing start marked")
	}
}

func TestCompetingCycleStartReplaceFailsWhenTheStoredStartCannotMove(t *testing.T) {
	earlier, selected, all := competingStartFixture()
	logs := &movingStartDayLogs{dayLogRepositoryStub: newDayLogRepositoryStub(), moveErr: errors.New("driver: move refused")}
	logs.entries["2026-03-08"] = earlier
	logs.entries["2026-03-10"] = selected
	service := NewDayService(logs, &dayUserRepositoryStub{settings: models.User{LastPeriodStart: &earlier.Date}})

	err := service.clearCompetingCycleStarts(context.Background(), 10, all, selected, competingStartNow, time.UTC)
	if !errors.Is(err, logs.moveErr) {
		t.Fatalf("expected the failed stored-start move back, got %v", err)
	}
	if logs.moves != 1 {
		t.Fatalf("expected exactly one move attempt, got %d", logs.moves)
	}
}

func TestCompetingCycleStartReplaceIsRefusedWithoutATransactionalStartMove(t *testing.T) {
	earlier, selected, all := competingStartFixture()
	logs := newDayLogRepositoryStub()
	logs.entries["2026-03-08"] = earlier
	logs.entries["2026-03-10"] = selected
	service := NewDayService(logs, &dayUserRepositoryStub{settings: models.User{LastPeriodStart: &earlier.Date}})

	err := service.clearCompetingCycleStarts(context.Background(), 10, all, selected, competingStartNow, time.UTC)
	if !errors.Is(err, errLastPeriodStartMoveUnsupported) {
		t.Fatalf("expected errLastPeriodStartMoveUnsupported, got %v", err)
	}
}
