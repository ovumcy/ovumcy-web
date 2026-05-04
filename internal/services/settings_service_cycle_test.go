package services

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestValidateCycleSettingsRejectsOutOfRangeAndIncompatibleValues(t *testing.T) {
	service := NewSettingsService(nil)
	now := time.Date(2026, time.February, 15, 12, 0, 0, 0, time.UTC)

	_, err := service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:  14,
		PeriodLength: 5,
	}, now, time.UTC)
	if !errors.Is(err, ErrSettingsCycleLengthOutOfRange) {
		t.Fatalf("expected ErrSettingsCycleLengthOutOfRange, got %v", err)
	}

	_, err = service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:  28,
		PeriodLength: 15,
	}, now, time.UTC)
	if !errors.Is(err, ErrSettingsPeriodLengthOutOfRange) {
		t.Fatalf("expected ErrSettingsPeriodLengthOutOfRange, got %v", err)
	}

	_, err = service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:  20,
		PeriodLength: 13,
	}, now, time.UTC)
	if !errors.Is(err, ErrSettingsPeriodLengthIncompatible) {
		t.Fatalf("expected ErrSettingsPeriodLengthIncompatible, got %v", err)
	}
}

func TestValidateCycleSettingsLastPeriodStartRules(t *testing.T) {
	service := NewSettingsService(nil)
	now := time.Date(2026, time.February, 15, 12, 0, 0, 0, time.UTC)

	_, err := service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:        28,
		PeriodLength:       5,
		LastPeriodStartSet: true,
		LastPeriodStartRaw: "invalid-date",
	}, now, time.UTC)
	if !errors.Is(err, ErrSettingsCycleStartDateInvalid) {
		t.Fatalf("expected ErrSettingsCycleStartDateInvalid for parse, got %v", err)
	}

	_, err = service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:        28,
		PeriodLength:       5,
		LastPeriodStartSet: true,
		LastPeriodStartRaw: "2025-12-31",
	}, now, time.UTC)
	if !errors.Is(err, ErrSettingsCycleStartDateInvalid) {
		t.Fatalf("expected ErrSettingsCycleStartDateInvalid for out-of-range old date, got %v", err)
	}

	_, err = service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:        28,
		PeriodLength:       5,
		LastPeriodStartSet: true,
		LastPeriodStartRaw: "2026-02-16",
	}, now, time.UTC)
	if !errors.Is(err, ErrSettingsCycleStartDateInvalid) {
		t.Fatalf("expected ErrSettingsCycleStartDateInvalid for out-of-range future date, got %v", err)
	}
}

func TestValidateCycleSettingsBuildsUpdate(t *testing.T) {
	service := NewSettingsService(nil)
	now := time.Date(2026, time.February, 15, 12, 0, 0, 0, time.UTC)

	update, err := service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:        28,
		PeriodLength:       6,
		AutoPeriodFill:     true,
		LastPeriodStartSet: true,
		LastPeriodStartRaw: "2026-02-10",
	}, now, time.UTC)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if update.LastPeriodStart == nil || update.LastPeriodStart.Format("2006-01-02") != "2026-02-10" {
		t.Fatalf("expected normalized last_period_start 2026-02-10, got %#v", update.LastPeriodStart)
	}
	if update.LastPeriodStart.Location() != time.UTC {
		t.Fatalf("expected canonical UTC location, got %s", update.LastPeriodStart.Location())
	}
	if update.LastPeriodStart.Hour() != 0 || update.LastPeriodStart.Minute() != 0 {
		t.Fatalf("expected UTC-midnight, got %s", update.LastPeriodStart.Format(time.RFC3339Nano))
	}

	cleared, err := service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:        28,
		PeriodLength:       6,
		AutoPeriodFill:     false,
		LastPeriodStartSet: true,
		LastPeriodStartRaw: "   ",
	}, now, time.UTC)
	if err != nil {
		t.Fatalf("expected nil error for clear, got %v", err)
	}
	if cleared.LastPeriodStart != nil {
		t.Fatalf("expected nil last_period_start, got %#v", cleared.LastPeriodStart)
	}
}

func TestValidateCycleSettingsCanonicalizesLastPeriodStartAcrossLocations(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load Asia/Tokyo: %v", err)
	}
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatalf("load America/Toronto: %v", err)
	}

	tests := []struct {
		name     string
		location *time.Location
	}{
		{name: "UTC+9 Tokyo", location: tokyo},
		{name: "UTC-5 Toronto", location: toronto},
	}

	service := NewSettingsService(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.February, 15, 12, 0, 0, 0, tt.location)

			update, err := service.ValidateCycleSettings(CycleSettingsValidationInput{
				CycleLength:        28,
				PeriodLength:       6,
				LastPeriodStartSet: true,
				LastPeriodStartRaw: "2026-02-10",
			}, now, tt.location)
			if err != nil {
				t.Fatalf("expected valid date, got %v", err)
			}
			if update.LastPeriodStart == nil {
				t.Fatal("expected non-nil last_period_start")
			}
			if update.LastPeriodStart.Location() != time.UTC {
				t.Fatalf("expected canonical UTC location regardless of input location, got %s", update.LastPeriodStart.Location())
			}
			if update.LastPeriodStart.Format("2006-01-02") != "2026-02-10" {
				t.Fatalf("expected calendar day 2026-02-10 preserved, got %s", update.LastPeriodStart.Format("2006-01-02"))
			}
			if update.LastPeriodStart.Hour() != 0 || update.LastPeriodStart.Minute() != 0 {
				t.Fatalf("expected UTC-midnight, got %s", update.LastPeriodStart.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestValidateCycleSettingsAcceptsLocalTodayInPositiveOffset(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load Asia/Tokyo: %v", err)
	}

	service := NewSettingsService(nil)
	now := time.Date(2026, time.February, 15, 12, 0, 0, 0, tokyo)

	update, err := service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:        28,
		PeriodLength:       6,
		LastPeriodStartSet: true,
		LastPeriodStartRaw: "2026-02-15",
	}, now, tokyo)
	if err != nil {
		t.Fatalf("expected today (UTC+9) to pass bounds check, got %v", err)
	}
	if update.LastPeriodStart == nil || update.LastPeriodStart.Format("2006-01-02") != "2026-02-15" {
		t.Fatalf("expected today canonicalized to 2026-02-15 UTC, got %#v", update.LastPeriodStart)
	}
	if update.LastPeriodStart.Location() != time.UTC {
		t.Fatalf("expected UTC location, got %s", update.LastPeriodStart.Location())
	}
}

func TestValidateCycleSettingsNormalizesOwnerPreferences(t *testing.T) {
	service := NewSettingsService(nil)
	now := time.Date(2026, time.February, 15, 12, 0, 0, 0, time.UTC)

	update, err := service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:        28,
		PeriodLength:       6,
		AutoPeriodFill:     true,
		UnpredictableCycle: true,
		AgeGroup:           "  AGE_35_PLUS  ",
		UsageGoal:          "  TRYING_TO_CONCEIVE  ",
	}, now, time.UTC)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !update.UnpredictableCycle {
		t.Fatalf("expected unpredictable cycle mode to be preserved")
	}
	if update.AgeGroup != models.AgeGroup35Plus {
		t.Fatalf("expected normalized age group %q, got %q", models.AgeGroup35Plus, update.AgeGroup)
	}
	if update.UsageGoal != models.UsageGoalTrying {
		t.Fatalf("expected normalized usage goal %q, got %q", models.UsageGoalTrying, update.UsageGoal)
	}
}

func TestApplyCycleSettings(t *testing.T) {
	service := NewSettingsService(nil)
	existingStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	user := &models.User{
		CycleLength:     30,
		PeriodLength:    7,
		AutoPeriodFill:  false,
		LastPeriodStart: &existingStart,
	}

	update := CycleSettingsUpdate{
		CycleLength:        28,
		PeriodLength:       5,
		AutoPeriodFill:     true,
		LastPeriodStartSet: true,
		LastPeriodStart:    nil,
	}
	service.ApplyCycleSettings(user, update)

	if user.CycleLength != 28 || user.PeriodLength != 5 || !user.AutoPeriodFill {
		t.Fatalf("expected updated cycle settings, got cycle=%d period=%d auto=%v", user.CycleLength, user.PeriodLength, user.AutoPeriodFill)
	}
	if user.LastPeriodStart != nil {
		t.Fatalf("expected cleared last period start, got %v", user.LastPeriodStart)
	}
}

func TestApplyCycleSettingsUpdatesOwnerPreferences(t *testing.T) {
	service := NewSettingsService(nil)
	user := &models.User{}

	service.ApplyCycleSettings(user, CycleSettingsUpdate{
		CycleLength:        31,
		PeriodLength:       6,
		AutoPeriodFill:     false,
		IrregularCycle:     true,
		UnpredictableCycle: true,
		AgeGroup:           "age_35_plus",
		UsageGoal:          "avoid_pregnancy",
	})

	if user.CycleLength != 31 || user.PeriodLength != 6 {
		t.Fatalf("expected updated cycle lengths, got cycle=%d period=%d", user.CycleLength, user.PeriodLength)
	}
	if user.AutoPeriodFill {
		t.Fatalf("expected AutoPeriodFill=false")
	}
	if !user.IrregularCycle || !user.UnpredictableCycle {
		t.Fatalf("expected irregular and unpredictable cycle flags to be updated, got irregular=%v unpredictable=%v", user.IrregularCycle, user.UnpredictableCycle)
	}
	if user.AgeGroup != models.AgeGroup35Plus {
		t.Fatalf("expected normalized age group %q, got %q", models.AgeGroup35Plus, user.AgeGroup)
	}
	if user.UsageGoal != models.UsageGoalAvoid {
		t.Fatalf("expected normalized usage goal %q, got %q", models.UsageGoalAvoid, user.UsageGoal)
	}
}

func TestSaveCycleSettingsPersistsNormalizedOwnerPreferences(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{}
	service := NewSettingsService(repo)
	lastPeriodStart := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	err := service.SaveCycleSettings(42, CycleSettingsUpdate{
		CycleLength:        29,
		PeriodLength:       5,
		AutoPeriodFill:     true,
		IrregularCycle:     false,
		UnpredictableCycle: true,
		AgeGroup:           "unexpected-age",
		UsageGoal:          "trying_to_conceive",
		LastPeriodStartSet: true,
		LastPeriodStart:    &lastPeriodStart,
	})
	if err != nil {
		t.Fatalf("SaveCycleSettings() unexpected error: %v", err)
	}
	if repo.updatedUserID != 42 {
		t.Fatalf("expected updated user id 42, got %d", repo.updatedUserID)
	}
	if repo.updates["unpredictable_cycle"] != true {
		t.Fatalf("expected unpredictable_cycle=true, got %#v", repo.updates["unpredictable_cycle"])
	}
	if repo.updates["age_group"] != models.AgeGroupUnknown {
		t.Fatalf("expected age_group fallback %q, got %#v", models.AgeGroupUnknown, repo.updates["age_group"])
	}
	if repo.updates["usage_goal"] != models.UsageGoalTrying {
		t.Fatalf("expected usage_goal %q, got %#v", models.UsageGoalTrying, repo.updates["usage_goal"])
	}
}
