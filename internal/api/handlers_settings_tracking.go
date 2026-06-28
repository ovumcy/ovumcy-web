package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

var trackingSettingsMutation = healthMutationKind{action: "settings.tracking_update", target: "tracking_settings"}

func (handler *Handler) UpdateTrackingSettings(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, trackingSettingsMutation, unauthorizedErrorSpec())
	}

	input, err := parseTrackingSettingsInput(c)
	if err != nil {
		return handler.failMutation(c, trackingSettingsMutation, settingsInvalidInputErrorSpec())
	}

	update := services.TrackingSettingsUpdate{
		TrackBBT:             input.TrackBBT,
		TemperatureUnit:      input.TemperatureUnit,
		TrackCervicalMucus:   input.TrackCervicalMucus,
		HideSexChip:          input.HideSexChip,
		HideCycleFactors:     input.HideCycleFactors,
		HideNotesField:       input.HideNotesField,
		ShowHistoricalPhases: input.ShowHistoricalPhases,
	}
	if err := handler.settingsService.SaveTrackingSettings(c.UserContext(), user.ID, update); err != nil {
		return handler.failMutation(c, trackingSettingsMutation, settingsTrackingUpdateErrorSpec())
	}

	handler.settingsService.ApplyTrackingSettings(user, update)
	status := services.SettingsTrackingUpdatedStatus
	handler.logMutationSuccess(c, trackingSettingsMutation)

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":                     true,
			"status":                 status,
			"track_bbt":              update.TrackBBT,
			"temperature_unit":       services.NormalizeTemperatureUnit(update.TemperatureUnit),
			"track_cervical_mucus":   update.TrackCervicalMucus,
			"hide_sex_chip":          update.HideSexChip,
			"hide_cycle_factors":     update.HideCycleFactors,
			"hide_notes_field":       update.HideNotesField,
			"show_historical_phases": update.ShowHistoricalPhases,
		})
	}
	if isHTMX(c) {
		return c.SendString(htmxSettingsSuccessMarkup(c, status, "Tracking settings updated successfully."))
	}

	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: status})
	return redirectOrJSON(c, "/settings")
}

func parseTrackingSettingsInput(c *fiber.Ctx) (trackingSettingsInput, error) {
	input := trackingSettingsInput{}
	if hasJSONBody(c) {
		if err := c.BodyParser(&input); err != nil {
			return trackingSettingsInput{}, err
		}
		return input, nil
	}

	return trackingSettingsInput{
		TrackBBT:             services.ParseBoolLike(c.FormValue("track_bbt")),
		TemperatureUnit:      c.FormValue("temperature_unit"),
		TrackCervicalMucus:   services.ParseBoolLike(c.FormValue("track_cervical_mucus")),
		HideSexChip:          services.ParseBoolLike(c.FormValue("hide_sex_chip")),
		HideCycleFactors:     services.ParseBoolLike(c.FormValue("hide_cycle_factors")),
		HideNotesField:       services.ParseBoolLike(c.FormValue("hide_notes_field")),
		ShowHistoricalPhases: services.ParseBoolLike(c.FormValue("show_historical_phases")),
	}, nil
}
