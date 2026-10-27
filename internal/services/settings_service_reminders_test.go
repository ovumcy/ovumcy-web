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

func TestSaveReminderLeadDaysRefusesOutOfRange(t *testing.T) {
	for _, raw := range []int{MinReminderLeadDays - 1, -5, MaxReminderLeadDays + 1, 99} {
		repo := &stubSettingsTrackingUserRepo{}
		service := NewSettingsService(repo)

		changed, err := service.SaveReminderLeadDays(context.Background(), 1, DefaultReminderLeadDays, raw)
		if !errors.Is(err, ErrReminderLeadDaysOutOfRange) {
			t.Fatalf("raw=%d: expected ErrReminderLeadDaysOutOfRange, got %v", raw, err)
		}
		if changed || repo.reminderCalls != 0 {
			t.Fatalf("raw=%d: a refused value must write nothing (changed=%v, calls=%d)", raw, changed, repo.reminderCalls)
		}
	}
}

func TestSaveReminderLeadDaysPersistsBoundaryValues(t *testing.T) {
	cases := []struct {
		name    string
		current int
		raw     int
		want    int
	}{
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

func TestSaveReminderLeadDaysNoOpWhenStoredValueWasOutOfRange(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{}
	service := NewSettingsService(repo)

	// A legacy stored value above the max reads as the max; saving the max is
	// then a no-op rather than a redundant write.
	changed, err := service.SaveReminderLeadDays(context.Background(), 1, 100, MaxReminderLeadDays)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when the new value equals the normalized current")
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
