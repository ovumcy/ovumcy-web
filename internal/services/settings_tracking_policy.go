package services

import "github.com/ovumcy/ovumcy-web/internal/models"

type TrackingSettingsUpdate struct {
	TrackBBT             bool
	TemperatureUnit      string
	TrackCervicalMucus   bool
	HideSexChip          bool
	HideCycleFactors     bool
	HideNotesField       bool
	ShowHistoricalPhases bool
}

func (service *SettingsService) ApplyTrackingSettings(user *models.User, update TrackingSettingsUpdate) {
	if user == nil {
		return
	}
	user.TrackBBT = update.TrackBBT
	user.TemperatureUnit = NormalizeTemperatureUnit(update.TemperatureUnit)
	user.TrackCervicalMucus = update.TrackCervicalMucus
	user.HideSexChip = update.HideSexChip
	user.HideCycleFactors = update.HideCycleFactors
	user.HideNotesField = update.HideNotesField
	user.ShowHistoricalPhases = update.ShowHistoricalPhases
}

func (service *SettingsService) ResolveTrackingUpdateStatus() string {
	return "tracking_updated"
}
