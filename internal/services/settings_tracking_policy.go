package services

import "github.com/terraincognita07/ovumcy/internal/models"

type TrackingSettingsUpdate struct {
	TrackBBT           bool
	TemperatureUnit    string
	TrackCervicalMucus bool
	HideSexChip        bool
}

func (service *SettingsService) ApplyTrackingSettings(user *models.User, update TrackingSettingsUpdate) {
	if user == nil {
		return
	}
	user.TrackBBT = update.TrackBBT
	user.TemperatureUnit = NormalizeTemperatureUnit(update.TemperatureUnit)
	user.TrackCervicalMucus = update.TrackCervicalMucus
	user.HideSexChip = update.HideSexChip
}

func (service *SettingsService) ResolveTrackingUpdateStatus() string {
	return "tracking_updated"
}
