package api

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// goalOnlyCycleSettingsRequest reports whether the request body carries the
// usage goal and no cycle geometry at all. It is a question about the shape of
// the request, not about the domain: what a goal-only save means is decided by
// SettingsService.SaveUsageGoal. Presence is read field by field — a zero
// cycle_length submitted explicitly is still a (rejected) full save, never a
// silent partial one.
func goalOnlyCycleSettingsRequest(c fiber.Ctx) (string, bool) {
	if hasJSONBody(c) {
		probe := struct {
			CycleLength  *int    `json:"cycle_length"`
			PeriodLength *int    `json:"period_length"`
			UsageGoal    *string `json:"usage_goal"`
		}{}
		if err := c.Bind().Body(&probe); err != nil {
			return "", false
		}
		if probe.UsageGoal == nil || probe.CycleLength != nil || probe.PeriodLength != nil {
			return "", false
		}
		return strings.TrimSpace(*probe.UsageGoal), true
	}

	args := c.Request().PostArgs()
	if !args.Has("usage_goal") || args.Has("cycle_length") || args.Has("period_length") {
		return "", false
	}
	return strings.TrimSpace(string(args.Peek("usage_goal"))), true
}

func (handler *Handler) parseCycleSettingsInput(c fiber.Ctx) (services.CycleSettingsUpdate, string) {
	input := cycleSettingsInput{}
	location := handler.requestLocation(c)

	if hasJSONBody(c) {
		if err := c.Bind().Body(&input); err != nil {
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
