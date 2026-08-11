package services

import "github.com/ovumcy/ovumcy-web/internal/models"

// TrackingSettingsUpdate is the tracking-settings save in the positive
// vocabulary: Visibility carries the three section toggles as "shown", and the
// stored inversion is applied once, in tracking_visibility.go.
type TrackingSettingsUpdate struct {
	TrackBBT             bool
	TemperatureUnit      string
	TrackCervicalMucus   bool
	Visibility           TrackingVisibility
	ShowHistoricalPhases bool
	WeekStartsOn         string
}

func (service *SettingsService) ApplyTrackingSettings(user *models.User, update TrackingSettingsUpdate) {
	if user == nil {
		return
	}
	user.TrackBBT = update.TrackBBT
	user.TemperatureUnit = NormalizeTemperatureUnit(update.TemperatureUnit)
	user.TrackCervicalMucus = update.TrackCervicalMucus
	update.Visibility.ApplyToUser(user)
	user.ShowHistoricalPhases = update.ShowHistoricalPhases
	user.WeekStartsOn = NormalizeWeekStart(update.WeekStartsOn)
}

// SettingsTrackingUpdatedStatus is the flash status emitted after a
// successful tracking-settings save (always the same outcome).
const SettingsTrackingUpdatedStatus = "tracking_updated"
