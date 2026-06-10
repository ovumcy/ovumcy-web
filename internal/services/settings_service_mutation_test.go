package services

import (
	"context"
	"testing"
	"time"
)

func TestSaveCycleSettingsPersistsExplicitLastPeriodStart(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{}
	service := NewSettingsService(repo)
	lastPeriodStart := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	err := service.SaveCycleSettings(context.Background(), 42, CycleSettingsUpdate{
		CycleLength:        28,
		PeriodLength:       5,
		LastPeriodStartSet: true,
		LastPeriodStart:    &lastPeriodStart,
	})
	if err != nil {
		t.Fatalf("SaveCycleSettings() unexpected error: %v", err)
	}

	raw, ok := repo.updates["last_period_start"]
	if !ok {
		t.Fatalf("expected last_period_start to be present in persisted updates, got %#v", repo.updates)
	}
	persisted, ok := raw.(time.Time)
	if !ok {
		t.Fatalf("expected last_period_start to persist the dereferenced time.Time value, got %#v", raw)
	}
	if !persisted.Equal(lastPeriodStart) {
		t.Fatalf("expected persisted last_period_start %s, got %s", lastPeriodStart, persisted)
	}
}
