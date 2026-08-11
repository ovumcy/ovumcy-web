package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

var trackingSettingsMutation = healthMutationKind{action: "settings.tracking_update", target: "tracking_settings"}

func (handler *Handler) UpdateTrackingSettings(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, trackingSettingsMutation, unauthorizedErrorSpec())
	}

	update, err := parseTrackingSettingsInput(c)
	if err != nil {
		return handler.failMutation(c, trackingSettingsMutation, settingsInvalidInputErrorSpec())
	}

	if err := handler.settingsService.SaveTrackingSettings(c.Context(), user.ID, update); err != nil {
		return handler.failMutation(c, trackingSettingsMutation, settingsTrackingUpdateErrorSpec())
	}

	handler.settingsService.ApplyTrackingSettings(user, update)
	status := services.SettingsTrackingUpdatedStatus
	handler.logMutationSuccess(c, trackingSettingsMutation)

	if acceptsJSON(c) {
		// The v1 response echoes the published (inverted) keys; the values come
		// from the services conversion point, never from a negation here.
		hidden := update.Visibility.HiddenColumns()
		return c.JSON(fiber.Map{
			"ok":                     true,
			"status":                 status,
			"track_bbt":              update.TrackBBT,
			"temperature_unit":       services.NormalizeTemperatureUnit(update.TemperatureUnit),
			"track_cervical_mucus":   update.TrackCervicalMucus,
			"hide_sex_chip":          hidden.HideSexChip,
			"hide_cycle_factors":     hidden.HideCycleFactors,
			"hide_notes_field":       hidden.HideNotesField,
			"show_historical_phases": update.ShowHistoricalPhases,
			"week_starts_on":         services.NormalizeWeekStart(update.WeekStartsOn),
		})
	}
	if isHTMX(c) {
		return c.SendString(htmxSettingsSuccessMarkup(c, status, "Tracking settings updated successfully."))
	}

	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: status})
	return redirectOrJSON(c, "/settings")
}

// parseTrackingSettingsInput reads the save in whichever vocabulary its client
// speaks and returns the positive service update.
//
// The published v1 JSON body keeps the inverted hide_* keys (renaming one is a
// breaking change) and is mapped through the services conversion point. The
// settings form — where the mixed polarity was a usability defect — posts the
// positive show_* fields the toggles are now labelled with, so a checked box
// means "visible" on every row and no second negation stands between the label
// and the column.
func parseTrackingSettingsInput(c fiber.Ctx) (services.TrackingSettingsUpdate, error) {
	if hasJSONBody(c) {
		input := trackingSettingsInput{}
		if err := c.Bind().Body(&input); err != nil {
			return services.TrackingSettingsUpdate{}, err
		}
		return services.TrackingSettingsUpdate{
			TrackBBT:           input.TrackBBT,
			TemperatureUnit:    input.TemperatureUnit,
			TrackCervicalMucus: input.TrackCervicalMucus,
			Visibility: services.TrackingVisibilityFromHiddenColumns(services.TrackingHiddenColumns{
				HideSexChip:      input.HideSexChip,
				HideCycleFactors: input.HideCycleFactors,
				HideNotesField:   input.HideNotesField,
			}),
			ShowHistoricalPhases: input.ShowHistoricalPhases,
			WeekStartsOn:         input.WeekStartsOn,
		}, nil
	}

	return services.TrackingSettingsUpdate{
		TrackBBT:           services.ParseBoolLike(c.FormValue("track_bbt")),
		TemperatureUnit:    c.FormValue("temperature_unit"),
		TrackCervicalMucus: services.ParseBoolLike(c.FormValue("track_cervical_mucus")),
		Visibility: services.TrackingVisibility{
			ShowSexChip:      services.ParseBoolLike(c.FormValue("show_sex_chip")),
			ShowCycleFactors: services.ParseBoolLike(c.FormValue("show_cycle_factors")),
			ShowNotesField:   services.ParseBoolLike(c.FormValue("show_notes_field")),
		},
		ShowHistoricalPhases: services.ParseBoolLike(c.FormValue("show_historical_phases")),
		WeekStartsOn:         c.FormValue("week_starts_on"),
	}, nil
}
