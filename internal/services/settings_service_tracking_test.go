package services

import (
	"errors"
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestApplyTrackingSettings(t *testing.T) {
	service := NewSettingsService(nil)
	user := &models.User{
		TrackBBT:           false,
		TrackCervicalMucus: false,
		HideSexChip:        false,
		TemperatureUnit:    "",
	}

	service.ApplyTrackingSettings(user, TrackingSettingsUpdate{
		TrackBBT:           true,
		TrackCervicalMucus: true,
		HideSexChip:        true,
		TemperatureUnit:    TemperatureUnitFahrenheit,
	})

	if !user.TrackBBT {
		t.Fatal("expected TrackBBT to be enabled")
	}
	if !user.TrackCervicalMucus {
		t.Fatal("expected TrackCervicalMucus to be enabled")
	}
	if !user.HideSexChip {
		t.Fatal("expected HideSexChip to be enabled")
	}
	if user.TemperatureUnit != TemperatureUnitFahrenheit {
		t.Fatalf("expected TemperatureUnit=%q, got %q", TemperatureUnitFahrenheit, user.TemperatureUnit)
	}
}

func TestResolveTrackingUpdateStatus(t *testing.T) {
	service := NewSettingsService(nil)

	if got := service.ResolveTrackingUpdateStatus(); got != "tracking_updated" {
		t.Fatalf("expected tracking_updated, got %q", got)
	}
}

func TestSaveTrackingSettings(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{}
	service := NewSettingsService(repo)

	err := service.SaveTrackingSettings(42, TrackingSettingsUpdate{
		TrackBBT:           true,
		TrackCervicalMucus: true,
		HideSexChip:        true,
		TemperatureUnit:    TemperatureUnitFahrenheit,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if repo.updatedUserID != 42 {
		t.Fatalf("expected updated user id 42, got %d", repo.updatedUserID)
	}
	if repo.updates["track_bbt"] != true {
		t.Fatalf("expected track_bbt=true, got %#v", repo.updates["track_bbt"])
	}
	if repo.updates["track_cervical_mucus"] != true {
		t.Fatalf("expected track_cervical_mucus=true, got %#v", repo.updates["track_cervical_mucus"])
	}
	if repo.updates["hide_sex_chip"] != true {
		t.Fatalf("expected hide_sex_chip=true, got %#v", repo.updates["hide_sex_chip"])
	}
	if repo.updates["temperature_unit"] != TemperatureUnitFahrenheit {
		t.Fatalf("expected temperature_unit=%q, got %#v", TemperatureUnitFahrenheit, repo.updates["temperature_unit"])
	}
}

func TestSaveTrackingSettingsPropagatesUpdateError(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{updateErr: errors.New("write failed")}
	service := NewSettingsService(repo)

	if err := service.SaveTrackingSettings(42, TrackingSettingsUpdate{}); err == nil {
		t.Fatal("expected update error")
	}
}

type stubSettingsTrackingUserRepo struct {
	updatedUserID uint
	updates       map[string]any
	updateErr     error
}

func (stub *stubSettingsTrackingUserRepo) UpdateDisplayName(uint, string) error {
	return nil
}

func (stub *stubSettingsTrackingUserRepo) UpdateRecoveryCodeHash(uint, string) error {
	return nil
}

func (stub *stubSettingsTrackingUserRepo) UpdatePasswordAndRevokeSessions(uint, string, bool) error {
	return nil
}

func (stub *stubSettingsTrackingUserRepo) UpdateByID(userID uint, updates map[string]any) error {
	stub.updatedUserID = userID
	stub.updates = updates
	return stub.updateErr
}

func (stub *stubSettingsTrackingUserRepo) LoadSettingsByID(uint) (models.User, error) {
	return models.User{}, nil
}

func (stub *stubSettingsTrackingUserRepo) ClearAllDataAndResetSettings(uint) error {
	return nil
}

func (stub *stubSettingsTrackingUserRepo) DeleteAccountAndRelatedData(uint) error {
	return nil
}
