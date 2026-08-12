package services

// usage_goal_autonomy_test.go — the usage goal is owner-declared and nothing
// else. These tests pin the two halves of that: the goal-only save writes
// exactly one column, and no health observation the owner logs may rewrite it.
// The pregnancy case is the one that matters — a positive test is precisely the
// event a "helpful" product would use to flip the mode to something else.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// usageGoalRecordingRepo is a full SettingsUserRepository whose only live
// method is UpdateByID; every update map is captured so a test can assert what
// the service wrote, not merely that it did not fail.
type usageGoalRecordingRepo struct {
	updates   []map[string]any
	userIDs   []uint
	updateErr error
}

func (repo *usageGoalRecordingRepo) UpdateDisplayName(context.Context, uint, string) error {
	return nil
}

func (repo *usageGoalRecordingRepo) UpdateInterfaceLanguage(context.Context, uint, string) (bool, error) {
	return true, nil
}

func (repo *usageGoalRecordingRepo) UpdateUserTimezone(context.Context, uint, string) error {
	return nil
}

func (repo *usageGoalRecordingRepo) UpdateReminderLeadDays(context.Context, uint, int) error {
	return nil
}

func (repo *usageGoalRecordingRepo) UpdatePasswordAndRevokeSessions(context.Context, uint, string, bool) error {
	return nil
}

func (repo *usageGoalRecordingRepo) UpdatePasswordRecoveryCodeAndRevokeSessions(context.Context, uint, string, string, bool) error {
	return nil
}

func (repo *usageGoalRecordingRepo) UpdateByID(_ context.Context, userID uint, updates map[string]any) error {
	repo.updates = append(repo.updates, updates)
	repo.userIDs = append(repo.userIDs, userID)
	return repo.updateErr
}

func (repo *usageGoalRecordingRepo) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return models.User{}, nil
}

func (repo *usageGoalRecordingRepo) ClearAllDataAndResetSettings(context.Context, uint) error {
	return nil
}

func (repo *usageGoalRecordingRepo) DeleteAccountAndRelatedData(context.Context, uint) error {
	return nil
}

func TestSaveUsageGoalWritesOnlyTheGoalColumn(t *testing.T) {
	repo := &usageGoalRecordingRepo{}
	service := NewSettingsService(repo)

	stored, err := service.SaveUsageGoal(context.Background(), 9, models.UsageGoalAvoid)
	if err != nil {
		t.Fatalf("SaveUsageGoal: %v", err)
	}
	if stored != models.UsageGoalAvoid {
		t.Fatalf("expected stored goal %q, got %q", models.UsageGoalAvoid, stored)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("expected exactly one update, got %d", len(repo.updates))
	}
	if repo.userIDs[0] != 9 {
		t.Fatalf("expected the write scoped to user 9, got %d", repo.userIDs[0])
	}
	if len(repo.updates[0]) != 1 {
		t.Fatalf("expected a single-column update, got %v", repo.updates[0])
	}
	if got := repo.updates[0]["usage_goal"]; got != models.UsageGoalAvoid {
		t.Fatalf("expected usage_goal=%q in the update, got %v", models.UsageGoalAvoid, got)
	}
}

func TestSaveUsageGoalFallsBackToTheNeutralDefault(t *testing.T) {
	repo := &usageGoalRecordingRepo{}
	service := NewSettingsService(repo)

	stored, err := service.SaveUsageGoal(context.Background(), 9, "not-a-mode")
	if err != nil {
		t.Fatalf("SaveUsageGoal: %v", err)
	}
	if stored != models.UsageGoalHealth {
		t.Fatalf("expected the neutral default %q, got %q", models.UsageGoalHealth, stored)
	}
	if got := repo.updates[0]["usage_goal"]; got != models.UsageGoalHealth {
		t.Fatalf("expected the normalized goal to be persisted, got %v", got)
	}
}

func TestSaveUsageGoalPropagatesTheRepositoryError(t *testing.T) {
	sentinel := errors.New("db unavailable")
	service := NewSettingsService(&usageGoalRecordingRepo{updateErr: sentinel})

	stored, err := service.SaveUsageGoal(context.Background(), 9, models.UsageGoalTrying)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the repository error to propagate, got %v", err)
	}
	if stored != "" {
		t.Fatalf("expected no goal reported on failure, got %q", stored)
	}
}

func TestApplyUsageGoalNormalizesAndToleratesAMissingUser(t *testing.T) {
	service := NewSettingsService(&usageGoalRecordingRepo{})

	service.ApplyUsageGoal(nil, models.UsageGoalAvoid)

	user := &models.User{UsageGoal: models.UsageGoalAvoid, CycleLength: 31}
	service.ApplyUsageGoal(user, "not-a-mode")
	if user.UsageGoal != models.UsageGoalHealth {
		t.Fatalf("expected an unknown goal to fall back to %q, got %q", models.UsageGoalHealth, user.UsageGoal)
	}
	if user.CycleLength != 31 {
		t.Fatalf("expected a goal-only apply to leave the cycle alone, got %d", user.CycleLength)
	}
}

func TestAlternativeUsageGoalsOffersEveryOtherMode(t *testing.T) {
	cases := map[string][]string{
		models.UsageGoalHealth: {models.UsageGoalAvoid, models.UsageGoalTrying},
		models.UsageGoalAvoid:  {models.UsageGoalTrying, models.UsageGoalHealth},
		models.UsageGoalTrying: {models.UsageGoalAvoid, models.UsageGoalHealth},
		// An unset column reads as the neutral default, so it offers the same
		// pair as an explicit "track my health".
		"": {models.UsageGoalAvoid, models.UsageGoalTrying},
	}

	for current, want := range cases {
		got := AlternativeUsageGoals(current)
		if len(got) != len(want) {
			t.Fatalf("AlternativeUsageGoals(%q): expected %v, got %v", current, want, got)
		}
		for index, goal := range want {
			if got[index] != goal {
				t.Fatalf("AlternativeUsageGoals(%q): expected %v, got %v", current, want, got)
			}
		}
	}
}

// usageGoalDayUserStub is a DayUserRepository that serves one owner's settings
// and records every write the day path issues.
type usageGoalDayUserStub struct {
	settings models.User
	updates  []map[string]any
}

func (stub *usageGoalDayUserStub) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return stub.settings, nil
}

func (stub *usageGoalDayUserStub) UpdateByID(_ context.Context, _ uint, updates map[string]any) error {
	stub.updates = append(stub.updates, updates)
	return nil
}

// TestPositivePregnancyTestNeverRewritesTheUsageGoal is the "never changed
// automatically" pin. Logging a positive pregnancy test is the strongest signal
// the product has that an owner trying to conceive may have succeeded — and it
// still may not touch the mode, which is the owner's declaration about what the
// interface should emphasise, not an inference from health data.
func TestPositivePregnancyTestNeverRewritesTheUsageGoal(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &usageGoalDayUserStub{settings: models.User{
		ID:             4,
		CycleLength:    28,
		PeriodLength:   5,
		AutoPeriodFill: true,
		UsageGoal:      models.UsageGoalTrying,
	}}
	service := NewDayService(logs, users)

	day := time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC)
	entry, err := service.UpsertDayEntryWithAutoFill(context.Background(), 4, day, DayEntryInput{
		Flow:          models.FlowNone,
		Mood:          MinDayMood,
		PregnancyTest: models.PregnancyTestPositive,
	}, time.UTC)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFill: %v", err)
	}
	// Positive anchor: the write really happened, so the absence below is about
	// the goal and not about a path that never ran.
	if entry.PregnancyTest != models.PregnancyTestPositive {
		t.Fatalf("expected the positive test to be stored, got %q", entry.PregnancyTest)
	}

	for _, update := range users.updates {
		if _, ok := update["usage_goal"]; ok {
			t.Fatalf("a day write must never touch usage_goal, got update %v", update)
		}
	}
	if users.settings.UsageGoal != models.UsageGoalTrying {
		t.Fatalf("expected the owner's mode to survive, got %q", users.settings.UsageGoal)
	}
}
