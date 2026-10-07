package services

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// onboardingFillNow is the fixed clock for the fill-bound cases: 2026-03-20 at
// noon UTC, so "today" is 2026-03-20 in UTC and in every zone within ±11h.
var onboardingFillNow = time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC)

// onboardWithPeriodFill drives onboarding through the REAL user repository on
// SQLite — step 1 with startDay, step 2 with a 5-day period and auto-fill on,
// then completion at now in location — and returns the calendar days stored
// as period days for the owner, in date order.
func onboardWithPeriodFill(t *testing.T, startDay time.Time, now time.Time, location *time.Location) []string {
	t.Helper()
	return onboardWithAutoPeriodFill(t, startDay, now, location, true)
}

func onboardWithAutoPeriodFill(t *testing.T, startDay time.Time, now time.Time, location *time.Location, autoPeriodFill bool) []string {
	t.Helper()

	database := newTwoOwnerIntegrationDatabase(t, "ovumcy-onboarding-fill-bound")
	service := NewOnboardingService(db.NewRepositories(database).Users)
	owner := createTwoOwnerUser(t, database, "onboarding-fill-bound@example.com", func(user *models.User) {
		user.OnboardingCompleted = false
	})

	ctx := context.Background()
	if err := service.SaveStep1(ctx, owner.ID, startDay); err != nil {
		t.Fatalf("SaveStep1() unexpected error: %v", err)
	}
	if _, _, err := service.SaveStep2(ctx, owner.ID, 28, 5, autoPeriodFill, false, models.UsageGoalHealth); err != nil {
		t.Fatalf("SaveStep2() unexpected error: %v", err)
	}
	if _, err := service.CompleteOnboardingForUser(ctx, owner.ID, now, location); err != nil {
		t.Fatalf("CompleteOnboardingForUser() unexpected error: %v", err)
	}

	var logs []models.DailyLog
	if err := database.Where("user_id = ? AND is_period = ?", owner.ID, true).Order("date ASC").Find(&logs).Error; err != nil {
		t.Fatalf("read period days: %v", err)
	}
	days := make([]string, 0, len(logs))
	for _, entry := range logs {
		days = append(days, entry.Date.UTC().Format("2006-01-02"))
	}

	completed := readTwoOwnerUser(t, database, owner.ID)
	if !completed.OnboardingCompleted || completed.LastPeriodStart == nil || !completed.LastPeriodStart.Equal(startDay) {
		t.Fatalf("expected onboarding completed on last_period_start %s, got completed=%v start=%v", startDay, completed.OnboardingCompleted, completed.LastPeriodStart)
	}
	return days
}

func requirePeriodDays(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("stored period days = %v, want %v", got, want)
	}
}

// TestOnboardingPeriodFillStopsAtToday pins the onboarding auto-fill to the
// owner's local today, the bound the day auto-fill already holds: a period
// started today, yesterday or the day before is recorded only through today,
// and one that ended in the past is recorded whole. The last_period_start is
// the owner's step-1 date in every case — only the seeded days are bounded.
func TestOnboardingPeriodFillStopsAtToday(t *testing.T) {
	cases := []struct {
		name  string
		start time.Time
		want  []string
	}{
		{
			name:  "period started today records today only",
			start: time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC),
			want:  []string{"2026-03-20"},
		},
		{
			name:  "period started two days ago records through today",
			start: time.Date(2026, time.March, 18, 0, 0, 0, 0, time.UTC),
			want:  []string{"2026-03-18", "2026-03-19", "2026-03-20"},
		},
		{
			name:  "period that ended in the past is recorded whole",
			start: time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
			want:  []string{"2026-03-10", "2026-03-11", "2026-03-12", "2026-03-13", "2026-03-14"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := onboardWithPeriodFill(t, testCase.start, onboardingFillNow, time.UTC)
			requirePeriodDays(t, got, testCase.want...)
		})
	}
}

// TestOnboardingPeriodFillStopsAtTheOwnersLocalToday_NonUTC holds the bound to
// the owner's calendar, not the server's, on both sides of UTC midnight. East
// of UTC the owner's today is already the next UTC day, so the fill reaches one
// day further than a UTC bound would; west of UTC it is still the previous UTC
// day, so the fill stops one day short of a UTC bound.
func TestOnboardingPeriodFillStopsAtTheOwnersLocalToday_NonUTC(t *testing.T) {
	start := time.Date(2026, time.March, 18, 0, 0, 0, 0, time.UTC)

	t.Run("east of UTC after local midnight", func(t *testing.T) {
		// Tokyo is UTC+9 with no DST: 2026-03-19 20:00 UTC is 2026-03-20 05:00
		// in Tokyo, where today is already 03-20. A UTC bound stops at 03-19.
		tokyo := time.FixedZone("Asia/Tokyo", 9*60*60)
		now := time.Date(2026, time.March, 19, 20, 0, 0, 0, time.UTC)
		got := onboardWithPeriodFill(t, start, now, tokyo)
		requirePeriodDays(t, got, "2026-03-18", "2026-03-19", "2026-03-20")
	})

	t.Run("west of UTC before local midnight", func(t *testing.T) {
		// UTC-5: 2026-03-20 03:00 UTC is 2026-03-19 22:00 locally, where today
		// is still 03-19. A UTC bound would also record 03-20.
		west := time.FixedZone("UTC-5", -5*60*60)
		now := time.Date(2026, time.March, 20, 3, 0, 0, 0, time.UTC)
		got := onboardWithPeriodFill(t, start, now, west)
		requirePeriodDays(t, got, "2026-03-18", "2026-03-19")
	})
}

// TestOnboardingCompletesWithoutFillWhenTheStartIsAfterTheOwnersToday covers
// a step-1 date that is already "tomorrow" in the zone completion runs in (a
// client that changed zone between the steps). The bound then ends before the
// start: no period day is written, and onboarding still completes on the
// owner's step-1 date instead of refusing.
func TestOnboardingCompletesWithoutFillWhenTheStartIsAfterTheOwnersToday(t *testing.T) {
	start := time.Date(2026, time.March, 21, 0, 0, 0, 0, time.UTC)
	got := onboardWithPeriodFill(t, start, onboardingFillNow, time.UTC)
	requirePeriodDays(t, got)
}

// TestOnboardingWithAutoFillOffWritesNoPeriodDay holds the toggle-off branch on
// the new bound: whether the period is still running or already over, an owner
// who switched auto-fill off gets no seeded day, and onboarding still completes
// on the step-1 date.
func TestOnboardingWithAutoFillOffWritesNoPeriodDay(t *testing.T) {
	for _, start := range []time.Time{
		time.Date(2026, time.March, 18, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
	} {
		t.Run(start.Format("2006-01-02"), func(t *testing.T) {
			got := onboardWithAutoPeriodFill(t, start, onboardingFillNow, time.UTC, false)
			requirePeriodDays(t, got)
		})
	}
}
