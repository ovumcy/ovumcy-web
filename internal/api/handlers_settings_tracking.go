package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) UpdateTrackingSettings(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logHealthDataMutationError(c, "settings.tracking_update", spec, "tracking_settings")
		return handler.respondMappedError(c, spec)
	}

	input, err := parseTrackingSettingsInput(c)
	if err != nil {
		spec := settingsInvalidInputErrorSpec()
		handler.logHealthDataMutationError(c, "settings.tracking_update", spec, "tracking_settings")
		return handler.respondMappedError(c, spec)
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
	if err := handler.settingsService.SaveTrackingSettings(user.ID, update); err != nil {
		spec := settingsTrackingUpdateErrorSpec()
		handler.logHealthDataMutationError(c, "settings.tracking_update", spec, "tracking_settings")
		return handler.respondMappedError(c, spec)
	}

	handler.settingsService.ApplyTrackingSettings(user, update)
	status := handler.settingsService.ResolveTrackingUpdateStatus()
	handler.logHealthDataMutation(c, "settings.tracking_update", "success", "tracking_settings")

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
		messageKey := services.SettingsStatusTranslationKey(status)
		message := translateMessage(currentMessages(c), messageKey)
		if message == "" || message == messageKey {
			message = "Tracking settings updated successfully."
		}
		return c.SendString(htmxDismissibleSuccessStatusMarkup(currentMessages(c), message))
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
