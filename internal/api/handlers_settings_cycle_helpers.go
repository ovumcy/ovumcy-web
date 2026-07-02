package api

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) parseCycleSettingsInput(c *fiber.Ctx) (services.CycleSettingsUpdate, string) {
	input := cycleSettingsInput{}
	location := handler.requestLocation(c)

	if hasJSONBody(c) {
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
			IrregularCycle:     services.ParseBoolLike(c.FormValue("irregular_cycle")),
			UnpredictableCycle: services.ParseBoolLike(c.FormValue("unpredictable_cycle")),
			AgeGroup:           strings.TrimSpace(c.FormValue("age_group")),
			UsageGoal:          strings.TrimSpace(c.FormValue("usage_goal")),
			LastPeriodStart:    strings.TrimSpace(c.FormValue("last_period_start")),
			LastPeriodStartSet: c.Request().PostArgs().Has("last_period_start"),
		}
	}
	update, err := handler.settingsService.ValidateCycleSettings(services.CycleSettingsValidationInput{
		CycleLength:        input.CycleLength,
		PeriodLength:       input.PeriodLength,
		AutoPeriodFill:     input.AutoPeriodFill,
		IrregularCycle:     input.IrregularCycle,
		UnpredictableCycle: input.UnpredictableCycle,
		AgeGroup:           input.AgeGroup,
		UsageGoal:          input.UsageGoal,
		LastPeriodStartRaw: input.LastPeriodStart,
		LastPeriodStartSet: input.LastPeriodStartSet,
	}, time.Now().In(location), location)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrSettingsCycleLengthOutOfRange):
			return services.CycleSettingsUpdate{}, "cycle length must be between 15 and 90"
		case errors.Is(err, services.ErrSettingsPeriodLengthOutOfRange):
			return services.CycleSettingsUpdate{}, "period length must be between 1 and 14"
		case errors.Is(err, services.ErrSettingsPeriodLengthIncompatible):
			return services.CycleSettingsUpdate{}, "period length is incompatible with cycle length"
		case errors.Is(err, services.ErrSettingsCycleStartDateInvalid):
			return services.CycleSettingsUpdate{}, "invalid cycle start date"
		default:
			return services.CycleSettingsUpdate{}, "invalid settings input"
		}
	}

	return update, ""
}
