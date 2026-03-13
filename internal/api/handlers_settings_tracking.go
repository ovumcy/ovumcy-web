package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) UpdateTrackingSettings(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	input, err := parseTrackingSettingsInput(c)
	if err != nil {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}

	update := services.TrackingSettingsUpdate{
		TrackBBT:           input.TrackBBT,
		TemperatureUnit:    input.TemperatureUnit,
		TrackCervicalMucus: input.TrackCervicalMucus,
		HideSexChip:        input.HideSexChip,
	}
	if err := handler.settingsService.SaveTrackingSettings(user.ID, update); err != nil {
		return handler.respondMappedError(c, settingsTrackingUpdateErrorSpec())
	}

	handler.settingsService.ApplyTrackingSettings(user, update)
	status := handler.settingsService.ResolveTrackingUpdateStatus()

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":                   true,
			"status":               status,
			"track_bbt":            update.TrackBBT,
			"temperature_unit":     services.NormalizeTemperatureUnit(update.TemperatureUnit),
			"track_cervical_mucus": update.TrackCervicalMucus,
			"hide_sex_chip":        update.HideSexChip,
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
		TrackBBT:           services.ParseBoolLike(c.FormValue("track_bbt")),
		TemperatureUnit:    c.FormValue("temperature_unit"),
		TrackCervicalMucus: services.ParseBoolLike(c.FormValue("track_cervical_mucus")),
		HideSexChip:        services.ParseBoolLike(c.FormValue("hide_sex_chip")),
	}, nil
}
