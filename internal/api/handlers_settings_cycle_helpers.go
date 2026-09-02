package api

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// cycleSettingsMemberNames are the wire names of every member this endpoint
// writes, in one place: both the goal-only predicate and the JSON patch read
// presence over the same set, so a member added to one and not the other cannot
// go unnoticed.
var cycleSettingsMemberNames = []string{
	"cycle_length",
	"period_length",
	"auto_period_fill",
	"irregular_cycle",
	"unpredictable_cycle",
	"age_group",
	"usage_goal",
	"last_period_start",
}

// goalOnlyCycleSettingsRequest reports whether the request body carries the
// usage goal and NOTHING else. It is a question about the shape of the request,
// not about the domain: what a goal-only save means is decided by
// SettingsService.SaveUsageGoal, which writes that one column. The predicate
// used to admit any body without the two lengths, so a mode switch arriving
// with `irregular_cycle` beside it dropped that flag on the floor.
func goalOnlyCycleSettingsRequest(c fiber.Ctx) (string, bool) {
	patch, ok := cycleSettingsPatchFromJSON(c)
	if ok {
		if patch.UsageGoal == nil {
			return "", false
		}
		if patch.CycleLength != nil || patch.PeriodLength != nil || patch.AutoPeriodFill != nil ||
			patch.IrregularCycle != nil || patch.UnpredictableCycle != nil || patch.AgeGroup != nil ||
			patch.LastPeriodStart != nil {
			return "", false
		}
		return strings.TrimSpace(*patch.UsageGoal), true
	}
	if hasJSONBody(c) {
		return "", false
	}

	args := c.Request().PostArgs()
	if !args.Has("usage_goal") {
		return "", false
	}
	for _, member := range cycleSettingsMemberNames {
		if member != "usage_goal" && args.Has(member) {
			return "", false
		}
	}
	return strings.TrimSpace(string(args.Peek("usage_goal"))), true
}

// cycleSettingsPatchFromJSON reads a JSON body member by member. The second
// result is false when the request carries no JSON body or the body does not
// decode; a body that decodes to nothing at all is still a valid (empty) patch.
func cycleSettingsPatchFromJSON(c fiber.Ctx) (services.CycleSettingsPatch, bool) {
	if !hasJSONBody(c) {
		return services.CycleSettingsPatch{}, false
	}
	probe := struct {
		CycleLength        *int    `json:"cycle_length"`
		PeriodLength       *int    `json:"period_length"`
		AutoPeriodFill     *bool   `json:"auto_period_fill"`
		IrregularCycle     *bool   `json:"irregular_cycle"`
		UnpredictableCycle *bool   `json:"unpredictable_cycle"`
		AgeGroup           *string `json:"age_group"`
		UsageGoal          *string `json:"usage_goal"`
		LastPeriodStart    *string `json:"last_period_start"`
	}{}
	if err := c.Bind().Body(&probe); err != nil {
		return services.CycleSettingsPatch{}, false
	}
	return services.CycleSettingsPatch(probe), true
}

func (handler *Handler) parseCycleSettingsInput(c fiber.Ctx, user *models.User) (services.CycleSettingsUpdate, string) {
	input := cycleSettingsInput{}
	location := handler.requestLocation(c)

	if hasJSONBody(c) {
		patch, ok := cycleSettingsPatchFromJSON(c)
		if !ok {
			return services.CycleSettingsUpdate{}, "invalid settings input"
		}
		resolved := handler.settingsService.ResolveCycleSettingsPatch(user, patch)
		update, err := handler.settingsService.ValidateCycleSettings(resolved, time.Now().In(location), location)
		if err != nil {
			return services.CycleSettingsUpdate{}, cycleSettingsValidationErrorKey(err)
		}
		return update, ""
	}

	// The settings form submits every control it owns on every save, and an
	// unchecked box submits nothing at all, so absence carries no meaning here:
	// a form body is read as the full snapshot it is.
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
		return services.CycleSettingsUpdate{}, cycleSettingsValidationErrorKey(err)
	}

	return update, ""
}

func cycleSettingsValidationErrorKey(err error) string {
	switch {
	case errors.Is(err, services.ErrSettingsCycleLengthOutOfRange):
		return "cycle length must be between 15 and 90"
	case errors.Is(err, services.ErrSettingsPeriodLengthOutOfRange):
		return "period length must be between 1 and 14"
	case errors.Is(err, services.ErrSettingsPeriodLengthIncompatible):
		return "period length is incompatible with cycle length"
	case errors.Is(err, services.ErrSettingsCycleStartDateInvalid):
		return "invalid cycle start date"
	default:
		return "invalid settings input"
	}
}
