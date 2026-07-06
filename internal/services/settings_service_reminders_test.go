package services

import (
	"context"
	"errors"
	"testing"
)

func TestSaveReminderLeadDaysPersistsClampedValueScopedToUser(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{}
	service := NewSettingsService(repo)

	changed, err := service.SaveReminderLeadDays(context.Background(), 42, DefaultReminderLeadDays, 7)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true when the value differs from current")
	}
	if repo.reminderCalls != 1 {
		t.Fatalf("expected exactly one persistence call, got %d", repo.reminderCalls)
	}
	if repo.reminderUpdatedUserID != 42 {
		t.Fatalf("expected write scoped to user 42, got %d", repo.reminderUpdatedUserID)
	}
	if repo.reminderLeadDaysPersisted != 7 {
		t.Fatalf("expected persisted lead days 7, got %d", repo.reminderLeadDaysPersisted)
	}
}

func TestSaveReminderLeadDaysClampsOutOfRange(t *testing.T) {
	cases := []struct {
		name    string
		current int
		raw     int
		want    int
	}{
		{name: "negative clamps to min", current: DefaultReminderLeadDays, raw: -5, want: MinReminderLeadDays},
		{name: "above max clamps to max", current: DefaultReminderLeadDays, raw: 99, want: MaxReminderLeadDays},
		{name: "min boundary persists", current: DefaultReminderLeadDays, raw: 0, want: 0},
		{name: "max boundary persists", current: DefaultReminderLeadDays, raw: 14, want: 14},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubSettingsTrackingUserRepo{}
			service := NewSettingsService(repo)

			changed, err := service.SaveReminderLeadDays(context.Background(), 1, tc.current, tc.raw)
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if !changed {
				t.Fatalf("expected changed=true for raw=%d", tc.raw)
			}
			if repo.reminderLeadDaysPersisted != tc.want {
				t.Fatalf("expected clamped persisted value %d, got %d", tc.want, repo.reminderLeadDaysPersisted)
			}
		})
	}
}

func TestSaveReminderLeadDaysNoOpWhenUnchanged(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{}
	service := NewSettingsService(repo)

	// Same clamped value as current → no DB write.
	changed, err := service.SaveReminderLeadDays(context.Background(), 1, 5, 5)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when the clamped value matches current")
	}
	if repo.reminderCalls != 0 {
		t.Fatalf("expected no persistence call for an unchanged value, got %d", repo.reminderCalls)
	}
}

func TestSaveReminderLeadDaysNoOpWhenOutOfRangeCurrentAlreadyAtBound(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{}
	service := NewSettingsService(repo)

	// current is already at the max; a raw value above the max clamps to the
	// same max, so it must be a no-op rather than a redundant write.
	changed, err := service.SaveReminderLeadDays(context.Background(), 1, MaxReminderLeadDays, 100)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when the clamped raw value equals the clamped current")
	}
	if repo.reminderCalls != 0 {
		t.Fatalf("expected no persistence call, got %d", repo.reminderCalls)
	}
}

func TestSaveReminderLeadDaysPropagatesPersistenceError(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{reminderErr: errors.New("write failed")}
	service := NewSettingsService(repo)

	changed, err := service.SaveReminderLeadDays(context.Background(), 1, DefaultReminderLeadDays, 9)
	if err == nil {
		t.Fatal("expected persistence error to propagate")
	}
	if changed {
		t.Fatal("expected changed=false when the write fails")
	}
}
