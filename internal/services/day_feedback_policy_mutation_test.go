package services

import (
	"context"
	"testing"
	"time"
)

func TestAcknowledgeLongPeriodWarningNormalizesCycleStartToMidnight(t *testing.T) {
	users := &dayUserRepositoryStub{}
	service := NewDayService(newDayLogRepositoryStub(), users)

	// A non-midnight instant: with a non-nil location the policy must rebuild
	// it at midnight in that location via CalendarDay before persisting. If the
	// non-nil branch is skipped, the raw 13:30 time-of-day leaks through.
	cycleStart := time.Date(2026, time.March, 1, 13, 30, 0, 0, time.UTC)

	if err := service.AcknowledgeLongPeriodWarning(context.Background(), 10, cycleStart, time.UTC); err != nil {
		t.Fatalf("AcknowledgeLongPeriodWarning() unexpected error: %v", err)
	}
	if users.settings.LongPeriodWarnedAt == nil {
		t.Fatal("expected long-period warning date to be persisted")
	}
	persisted := *users.settings.LongPeriodWarnedAt
	if persisted.Hour() != 0 || persisted.Minute() != 0 || persisted.Second() != 0 || persisted.Nanosecond() != 0 {
		t.Fatalf("expected persisted cycle start normalized to midnight, got %s", persisted.Format("2006-01-02T15:04:05"))
	}
	if got := persisted.Format("2006-01-02"); got != "2026-03-01" {
		t.Fatalf("expected persisted calendar day 2026-03-01, got %s", got)
	}
}
