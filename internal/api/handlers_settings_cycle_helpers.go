package api

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// cycleSettingsPatchProbe is the wire shape of a cycle-settings body, one
// pointer per member so an absent member is distinguishable from a zero one. It
// is the single declaration of the member set: the JSON arm binds into it and
// the form arm reads its json tags, so a member added here reaches both without
// a second list to keep in step.
type cycleSettingsPatchProbe struct {
	CycleLength        *int    `json:"cycle_length"`
	PeriodLength       *int    `json:"period_length"`
	AutoPeriodFill     *bool   `json:"auto_period_fill"`
	IrregularCycle     *bool   `json:"irregular_cycle"`
	UnpredictableCycle *bool   `json:"unpredictable_cycle"`
	AgeGroup           *string `json:"age_group"`
	UsageGoal          *string `json:"usage_goal"`
	LastPeriodStart    *string `json:"last_period_start"`
}

// cycleSettingsMemberNames are those wire names, derived rather than retyped.
var cycleSettingsMemberNames = cycleSettingsProbeMemberNames()

func cycleSettingsProbeMemberNames() []string {
	probeType := reflect.TypeOf(cycleSettingsPatchProbe{})
	names := make([]string, 0, probeType.NumField())
	for index := range probeType.NumField() {
		if name := probeType.Field(index).Tag.Get("json"); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// patchCarriesOnlyUsageGoal reports whether the goal is the one member the body
// names. It is a separate function so the sweep can drive it field by field:
// a member added to cycleSettingsPatchProbe and forgotten here would send a body
// carrying it back down the one-column path, dropping it — the very defect the
// partial save exists to end. Regression:
// TestGoalOnlyShortcutYieldsToEveryOtherMember.
func patchCarriesOnlyUsageGoal(patch services.CycleSettingsPatch) bool {
	if patch.UsageGoal == nil {
		return false
	}
	return patch.CycleLength == nil &&
		patch.PeriodLength == nil &&
		patch.AutoPeriodFill == nil &&
		patch.IrregularCycle == nil &&
		patch.UnpredictableCycle == nil &&
		patch.AgeGroup == nil &&
		patch.LastPeriodStart == nil
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
		if !patchCarriesOnlyUsageGoal(patch) {
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
	probe := cycleSettingsPatchProbe{}
	if err := c.Bind().Body(&probe); err != nil {
		return services.CycleSettingsPatch{}, false
	}
	return services.CycleSettingsPatch(probe), true
}

func (handler *Handler) parseCycleSettingsInput(c fiber.Ctx, user *models.User) (services.CycleSettingsUpdate, string) {
	location := handler.requestLocation(c)

	if hasJSONBody(c) {
		patch, ok := cycleSettingsPatchFromJSON(c)
		if !ok {
			return services.CycleSettingsUpdate{}, "invalid settings input"
		}
		resolved := handler.settingsService.ResolveCycleSettingsPatch(user, patch)
		// A body naming no member at all is refused rather than answered 200.
		// Before the save became partial it was refused too — as an out-of-range
		// cycle_length — and accepting it now would audit a cycle-settings change
		// that did not happen, which is the one thing the mutation log must not
		// say. SaveCycleSettings keeps its own empty-update floor underneath.
		if resolved.Present == (services.CycleSettingsMembers{}) && !resolved.LastPeriodStartSet {
			return services.CycleSettingsUpdate{}, "invalid settings input"
		}
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
	update, err := handler.settingsService.ValidateCycleSettings(services.CycleSettingsValidationInput{
		CycleLength:        cycleLength,
		PeriodLength:       periodLength,
		AutoPeriodFill:     services.ParseBoolLike(c.FormValue("auto_period_fill")),
		IrregularCycle:     services.ParseBoolLike(c.FormValue("irregular_cycle")),
		UnpredictableCycle: services.ParseBoolLike(c.FormValue("unpredictable_cycle")),
		AgeGroup:           strings.TrimSpace(c.FormValue("age_group")),
		UsageGoal:          strings.TrimSpace(c.FormValue("usage_goal")),
		LastPeriodStartRaw: strings.TrimSpace(c.FormValue("last_period_start")),
		LastPeriodStartSet: c.Request().PostArgs().Has("last_period_start"),
		// A form body is a full snapshot by construction, so every member is
		// present and every column is the form's to write.
		Present: services.AllCycleSettingsMembers(),
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
