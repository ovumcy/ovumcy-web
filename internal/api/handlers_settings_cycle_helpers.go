package api

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) parseCycleSettingsInput(c *fiber.Ctx) (services.CycleSettingsUpdate, string) {
	input := cycleSettingsInput{}

	contentType := strings.ToLower(c.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		if err := c.BodyParser(&input); err != nil {
			return services.CycleSettingsUpdate{}, "invalid settings input"
		}
		input.LastPeriodStart = strings.TrimSpace(input.LastPeriodStart)
		if input.LastPeriodStart != "" {
			input.LastPeriodStartSet = true
		}
	} else {
		cycleLength, err := strconv.Atoi(strings.TrimSpace(c.FormValue("cycle_length")))
		if err != nil {
			return services.CycleSettingsUpdate{}, "invalid settings input"
		}
		periodLength, err := strconv.Atoi(strings.TrimSpace(c.FormValue("period_length")))
		if err != nil {
			return services.CycleSettingsUpdate{}, "invalid settings input"
		}
		input = cycleSettingsInput{
			CycleLength:        cycleLength,
			PeriodLength:       periodLength,
			AutoPeriodFill:     services.ParseBoolLike(c.FormValue("auto_period_fill")),
			LastPeriodStart:    strings.TrimSpace(c.FormValue("last_period_start")),
			LastPeriodStartSet: c.Request().PostArgs().Has("last_period_start"),
		}
	}
	update, err := handler.settingsService.ValidateCycleSettings(services.CycleSettingsValidationInput{
		CycleLength:        input.CycleLength,
		PeriodLength:       input.PeriodLength,
		AutoPeriodFill:     input.AutoPeriodFill,
		LastPeriodStartRaw: input.LastPeriodStart,
		LastPeriodStartSet: input.LastPeriodStartSet,
	}, time.Now().In(handler.location), handler.location)
	if err != nil {
		switch services.ClassifySettingsCycleValidationError(err) {
		case services.SettingsCycleValidationErrorCycleLengthOutOfRange:
			return services.CycleSettingsUpdate{}, "cycle length must be between 15 and 90"
		case services.SettingsCycleValidationErrorPeriodLengthOutOfRange:
			return services.CycleSettingsUpdate{}, "period length must be between 1 and 14"
		case services.SettingsCycleValidationErrorPeriodLengthIncompatible:
			return services.CycleSettingsUpdate{}, "period length is incompatible with cycle length"
		case services.SettingsCycleValidationErrorCycleStartDateInvalid:
			return services.CycleSettingsUpdate{}, "invalid cycle start date"
		default:
			return services.CycleSettingsUpdate{}, "invalid settings input"
		}
	}

	return update, ""
}
