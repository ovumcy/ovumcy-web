package services

// day_feedback_utc_regression_test.go — F8 regression.
//
// AcknowledgeLongPeriodWarning called CalendarDay(cycleStart, location) to get
// a location-local midnight, then passed that value directly to
// UpdateByID(map{"long_period_warning_cycle_start": cycleStart}).
// Updates(map) bypasses the GORM BeforeSave hook that normally canonicalizes
// date-only columns to UTC-midnight, so a non-UTC offset was written to the DB
// (issue #48/#64 bug class).
//
// The fix rebuilds the value at time.UTC before the update.  These tests
// assert the persisted LongPeriodWarnedAt is always UTC-midnight regardless of
// the request timezone.

import (
	"context"
	"testing"
	"time"
)

// TestAcknowledgeLongPeriodWarningPersistsUTCMidnightInUTCZone is the baseline:
// a UTC request should still produce a UTC-midnight value.
func TestAcknowledgeLongPeriodWarningPersistsUTCMidnightInUTCZone(t *testing.T) {
	users := &dayUserRepositoryStub{}
	service := NewDayService(newDayLogRepositoryStub(), users)

	cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	if err := service.AcknowledgeLongPeriodWarning(context.Background(), 10, cycleStart, time.UTC); err != nil {
		t.Fatalf("AcknowledgeLongPeriodWarning() unexpected error: %v", err)
	}

	stored := users.settings.LongPeriodWarnedAt
	if stored == nil {
		t.Fatal("expected long_period_warning_cycle_start to be persisted, got nil")
	}
	if stored.Location() != time.UTC {
		t.Errorf("persisted value location = %v, want UTC", stored.Location())
	}
	if stored.Hour() != 0 || stored.Minute() != 0 || stored.Second() != 0 {
		t.Errorf("persisted value is not midnight: %v", stored)
	}
	if stored.Format("2006-01-02") != "2026-03-01" {
		t.Errorf("persisted calendar date = %s, want 2026-03-01", stored.Format("2006-01-02"))
	}
}

// TestAcknowledgeLongPeriodWarningPersistsUTCMidnightFromEastZone verifies that
// a cycleStart carrying a UTC+5 offset is canonicalized to UTC-midnight of the
// same calendar day before storage.  Without the fix the stored value would
// have been 2026-03-01T00:00:00+05:00 (i.e. 2026-02-28T19:00:00Z), which is
// not UTC-midnight and would produce an off-by-one on subsequent reads in any
// UTC-minus locale.
func TestAcknowledgeLongPeriodWarningPersistsUTCMidnightFromEastZone(t *testing.T) {
	users := &dayUserRepositoryStub{}
	service := NewDayService(newDayLogRepositoryStub(), users)

	east := time.FixedZone("UTC+5", 5*60*60)
	// Simulates what the handler sends: the cycle-start as a local midnight in
	// a UTC+5 request.
	cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, east)

	if err := service.AcknowledgeLongPeriodWarning(context.Background(), 10, cycleStart, east); err != nil {
		t.Fatalf("AcknowledgeLongPeriodWarning() unexpected error: %v", err)
	}

	stored := users.settings.LongPeriodWarnedAt
	if stored == nil {
		t.Fatal("expected long_period_warning_cycle_start to be persisted, got nil")
	}
	// Must be UTC-midnight regardless of the request timezone.
	if stored.Location() != time.UTC {
		t.Errorf("persisted value location = %v, want UTC", stored.Location())
	}
	if stored.Hour() != 0 || stored.Minute() != 0 || stored.Second() != 0 {
		t.Errorf("persisted value is not midnight: %v", stored)
	}
	// Calendar day must be the same day as the input, not shifted.
	if stored.Format("2006-01-02") != "2026-03-01" {
		t.Errorf("persisted calendar date = %s, want 2026-03-01 (must not shift across timezone boundary)", stored.Format("2006-01-02"))
	}
}

// TestAcknowledgeLongPeriodWarningPersistsUTCMidnightFromWestZone verifies the
// UTC-minus case.  A cycleStart in UTC-5 (2026-03-01T00:00:00-05:00) must also
// store as 2026-03-01T00:00:00Z, not 2026-03-01T05:00:00Z.
func TestAcknowledgeLongPeriodWarningPersistsUTCMidnightFromWestZone(t *testing.T) {
	users := &dayUserRepositoryStub{}
	service := NewDayService(newDayLogRepositoryStub(), users)

	west := time.FixedZone("UTC-5", -5*60*60)
	cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, west)

	if err := service.AcknowledgeLongPeriodWarning(context.Background(), 10, cycleStart, west); err != nil {
		t.Fatalf("AcknowledgeLongPeriodWarning() unexpected error: %v", err)
	}

	stored := users.settings.LongPeriodWarnedAt
	if stored == nil {
		t.Fatal("expected long_period_warning_cycle_start to be persisted, got nil")
	}
	if stored.Location() != time.UTC {
		t.Errorf("persisted value location = %v, want UTC", stored.Location())
	}
	if stored.Hour() != 0 || stored.Minute() != 0 || stored.Second() != 0 {
		t.Errorf("persisted value is not midnight: %v", stored)
	}
	if stored.Format("2006-01-02") != "2026-03-01" {
		t.Errorf("persisted calendar date = %s, want 2026-03-01", stored.Format("2006-01-02"))
	}
}
