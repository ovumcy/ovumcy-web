package services

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestSaveCycleSettingsWritesOnlyTheMembersItWasGiven is the persistence half of
// the partial save. Resolving an omitted member from the stored record and then
// writing it back is not the same as leaving it alone: the value written is the
// one read when this request authenticated, so a save that named only the cycle
// geometry would revert a tracking mode another request set in between —
// reverting a setting it never mentioned, which is the whole defect.
func TestSaveCycleSettingsWritesOnlyTheMembersItWasGiven(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{}
	service := NewSettingsService(repo)

	update := CycleSettingsUpdate{
		Present:     CycleSettingsMembers{CycleLength: true, PeriodLength: true},
		CycleLength: 29,
		// Carried but not named: a correct save must not write these.
		UsageGoal: models.UsageGoalHealth,
		AgeGroup:  models.AgeGroupUnknown,
	}
	update.PeriodLength = 5

	if err := service.SaveCycleSettings(context.Background(), 42, update); err != nil {
		t.Fatalf("SaveCycleSettings() unexpected error: %v", err)
	}

	assertUpdatedColumns(t, repo.updates, []string{"cycle_length", "period_length"})
}

// TestSaveCycleSettingsWritesTheAnchorItWasGiven pins the one member that
// carries its own presence flag rather than riding in CycleSettingsMembers.
func TestSaveCycleSettingsWritesTheAnchorItWasGiven(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{}
	service := NewSettingsService(repo)
	anchor := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	update := CycleSettingsUpdate{
		Present:            CycleSettingsMembers{UsageGoal: true},
		UsageGoal:          models.UsageGoalAvoid,
		LastPeriodStartSet: true,
		LastPeriodStart:    &anchor,
	}

	if err := service.SaveCycleSettings(context.Background(), 42, update); err != nil {
		t.Fatalf("SaveCycleSettings() unexpected error: %v", err)
	}

	assertUpdatedColumns(t, repo.updates, []string{"last_period_start", "usage_goal"})
}

// TestSaveCycleSettingsNamingNothingReachesNoRepository covers the empty answer:
// a body that named no member asked for no change, which is not an error and
// must not become one at the driver, where an empty update is a different
// question entirely.
func TestSaveCycleSettingsNamingNothingReachesNoRepository(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{}
	service := NewSettingsService(repo)

	if err := service.SaveCycleSettings(context.Background(), 42, CycleSettingsUpdate{}); err != nil {
		t.Fatalf("SaveCycleSettings() unexpected error: %v", err)
	}
	if repo.updates != nil {
		t.Fatalf("a save naming no member still reached the repository with %v", repo.updates)
	}
	if repo.updatedUserID != 0 {
		t.Fatalf("a save naming no member still named a user: %d", repo.updatedUserID)
	}
}

// TestResolveCycleSettingsPatchWithoutAStoredRecordNamesOnlyWhatItCarries
// covers the operand the handler always supplies and a future caller might not:
// with no stored record to resolve against, the members the body carries still
// win and the rest stay unnamed rather than being invented from a zero user.
func TestResolveCycleSettingsPatchWithoutAStoredRecordNamesOnlyWhatItCarries(t *testing.T) {
	service := NewSettingsService(nil)
	goal := models.UsageGoalTrying

	resolved := service.ResolveCycleSettingsPatch(nil, CycleSettingsPatch{UsageGoal: &goal})

	if resolved.UsageGoal != models.UsageGoalTrying {
		t.Fatalf("expected the carried goal, got %q", resolved.UsageGoal)
	}
	if resolved.Present != (CycleSettingsMembers{UsageGoal: true}) {
		t.Fatalf("expected only the goal named, got %+v", resolved.Present)
	}
	if resolved.LastPeriodStartSet {
		t.Fatal("expected the anchor to stay unnamed")
	}
}

func assertUpdatedColumns(t *testing.T, updates map[string]any, want []string) {
	t.Helper()

	got := make([]string, 0, len(updates))
	for column := range updates {
		got = append(got, column)
	}
	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("expected exactly %v written, got %v", want, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected exactly %v written, got %v", want, got)
		}
	}
}
