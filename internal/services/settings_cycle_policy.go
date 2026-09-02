package services

import (
	"errors"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrSettingsCycleLengthOutOfRange    = errors.New("settings cycle length out of range")
	ErrSettingsPeriodLengthOutOfRange   = errors.New("settings period length out of range")
	ErrSettingsPeriodLengthIncompatible = errors.New("settings period length incompatible with cycle length")
	ErrSettingsCycleStartDateInvalid    = errors.New("settings cycle start date invalid")
)

// CycleSettingsMembers records which cycle columns a save may write. A save
// writes the members it names and no others, so a request that never mentioned
// the tracking mode cannot revert a mode another request set between this one's
// authentication and its write — the omitted-member promise held across two
// overlapping requests, not only inside one.
//
// last_period_start is absent here on purpose: it carries its own
// LastPeriodStartSet, which already distinguishes "not sent" from "sent empty",
// a distinction the other members do not have.
type CycleSettingsMembers struct {
	CycleLength        bool
	PeriodLength       bool
	AutoPeriodFill     bool
	IrregularCycle     bool
	UnpredictableCycle bool
	AgeGroup           bool
	UsageGoal          bool
}

// AllCycleSettingsMembers marks every member present: the answer for a full
// snapshot, which is what the settings form submits.
func AllCycleSettingsMembers() CycleSettingsMembers {
	return CycleSettingsMembers{
		CycleLength:        true,
		PeriodLength:       true,
		AutoPeriodFill:     true,
		IrregularCycle:     true,
		UnpredictableCycle: true,
		AgeGroup:           true,
		UsageGoal:          true,
	}
}

type CycleSettingsValidationInput struct {
	CycleLength        int
	PeriodLength       int
	AutoPeriodFill     bool
	IrregularCycle     bool
	UnpredictableCycle bool
	AgeGroup           string
	UsageGoal          string
	LastPeriodStartRaw string
	LastPeriodStartSet bool
	// Present names the members this input actually carries. The zero value
	// writes nothing, so a caller building this by hand states its answer.
	Present CycleSettingsMembers
}

// CycleSettingsPatch is a cycle-settings body read member by member: a nil field
// was not in the body at all, which is a different fact from a field carrying a
// zero value. Only a transport that can express the difference builds one — an
// HTML form cannot, since an unchecked box is simply not submitted and would
// read as "leave it alone", so the form surface keeps sending a full snapshot.
type CycleSettingsPatch struct {
	CycleLength        *int
	PeriodLength       *int
	AutoPeriodFill     *bool
	IrregularCycle     *bool
	UnpredictableCycle *bool
	AgeGroup           *string
	UsageGoal          *string
	// A nil LastPeriodStart leaves the stored anchor alone, and so does an
	// empty string: that is what the JSON surface has always meant by it, and
	// this change is not the place to give an existing spelling a destructive
	// new reading. Clearing the anchor stays a form-surface operation, which
	// says it with a present-but-empty field.
	LastPeriodStart *string
}

// ResolveCycleSettingsPatch answers what a partial cycle save means: every
// member the body omitted keeps the value the account already holds, and only
// what was actually sent can change. Absence used to mean the field's zero
// value, so a caller saving the cycle lengths alone silently rewrote
// `usage_goal` to `health` — turning an owner tracking to avoid pregnancy into
// one tracking for general health, which reframes the fertile window across
// every surface — and wiped `age_group` back to unknown the same way.
//
// It lives here rather than in the transport layer because what an absent field
// means is a question about the account, not about HTTP: the resolution reads
// the stored record.
func (service *SettingsService) ResolveCycleSettingsPatch(current *models.User, patch CycleSettingsPatch) CycleSettingsValidationInput {
	stored := models.User{}
	if current != nil {
		stored = *current
	}

	resolved := CycleSettingsValidationInput{
		CycleLength:        stored.CycleLength,
		PeriodLength:       stored.PeriodLength,
		AutoPeriodFill:     stored.AutoPeriodFill,
		IrregularCycle:     stored.IrregularCycle,
		UnpredictableCycle: stored.UnpredictableCycle,
		AgeGroup:           stored.AgeGroup,
		UsageGoal:          stored.UsageGoal,
	}
	if patch.CycleLength != nil {
		resolved.CycleLength = *patch.CycleLength
		resolved.Present.CycleLength = true
	}
	if patch.PeriodLength != nil {
		resolved.PeriodLength = *patch.PeriodLength
		resolved.Present.PeriodLength = true
	}
	if patch.AutoPeriodFill != nil {
		resolved.AutoPeriodFill = *patch.AutoPeriodFill
		resolved.Present.AutoPeriodFill = true
	}
	if patch.IrregularCycle != nil {
		resolved.IrregularCycle = *patch.IrregularCycle
		resolved.Present.IrregularCycle = true
	}
	if patch.UnpredictableCycle != nil {
		resolved.UnpredictableCycle = *patch.UnpredictableCycle
		resolved.Present.UnpredictableCycle = true
	}
	if patch.AgeGroup != nil {
		resolved.AgeGroup = *patch.AgeGroup
		resolved.Present.AgeGroup = true
	}
	if patch.UsageGoal != nil {
		resolved.UsageGoal = *patch.UsageGoal
		resolved.Present.UsageGoal = true
	}
	if patch.LastPeriodStart != nil && strings.TrimSpace(*patch.LastPeriodStart) != "" {
		resolved.LastPeriodStartRaw = *patch.LastPeriodStart
		resolved.LastPeriodStartSet = true
	}
	return resolved
}

func (service *SettingsService) ValidateCycleSettings(input CycleSettingsValidationInput, now time.Time, location *time.Location) (CycleSettingsUpdate, error) {
	if !IsValidOnboardingCycleLength(input.CycleLength) {
		return CycleSettingsUpdate{}, ErrSettingsCycleLengthOutOfRange
	}
	if !IsValidOnboardingPeriodLength(input.PeriodLength) {
		return CycleSettingsUpdate{}, ErrSettingsPeriodLengthOutOfRange
	}

	if !IsCompatibleCycleAndPeriod(input.CycleLength, input.PeriodLength) {
		return CycleSettingsUpdate{}, ErrSettingsPeriodLengthIncompatible
	}

	update := CycleSettingsUpdate{
		Present:            input.Present,
		CycleLength:        input.CycleLength,
		PeriodLength:       input.PeriodLength,
		AutoPeriodFill:     input.AutoPeriodFill,
		IrregularCycle:     input.IrregularCycle,
		UnpredictableCycle: input.UnpredictableCycle,
		AgeGroup:           NormalizeAgeGroup(input.AgeGroup),
		UsageGoal:          NormalizeUsageGoal(input.UsageGoal),
		LastPeriodStartSet: input.LastPeriodStartSet,
	}

	if !input.LastPeriodStartSet {
		return update, nil
	}

	rawDate := strings.TrimSpace(input.LastPeriodStartRaw)
	if rawDate == "" {
		update.LastPeriodStart = nil
		return update, nil
	}

	if location == nil {
		location = time.UTC
	}
	parsedDay, err := ParseDayDate(rawDate, location)
	if err != nil {
		return CycleSettingsUpdate{}, ErrSettingsCycleStartDateInvalid
	}

	minCycleStart, today := SettingsCycleStartDateBounds(now, location)
	if parsedDay.Before(minCycleStart) || parsedDay.After(today) {
		return CycleSettingsUpdate{}, ErrSettingsCycleStartDateInvalid
	}

	// Stored date-only field: canonicalize to UTC midnight of the same
	// calendar day (see day_utils.go on the two shapes).
	canonical := CalendarDay(parsedDay, time.UTC)
	update.LastPeriodStart = &canonical
	return update, nil
}

func (service *SettingsService) ApplyCycleSettings(user *models.User, update CycleSettingsUpdate) {
	if user == nil {
		return
	}

	user.CycleLength = update.CycleLength
	user.PeriodLength = update.PeriodLength
	user.AutoPeriodFill = update.AutoPeriodFill
	user.IrregularCycle = update.IrregularCycle
	user.UnpredictableCycle = update.UnpredictableCycle
	user.AgeGroup = NormalizeAgeGroup(update.AgeGroup)
	user.UsageGoal = NormalizeUsageGoal(update.UsageGoal)

	if !update.LastPeriodStartSet {
		return
	}
	if update.LastPeriodStart == nil {
		user.LastPeriodStart = nil
		return
	}

	day := *update.LastPeriodStart
	user.LastPeriodStart = &day
}

// ApplyUsageGoal mirrors a goal-only save onto the in-request user so the rest
// of the response is framed by the mode just chosen, without touching any other
// cycle field (the partial counterpart of ApplyCycleSettings).
func (service *SettingsService) ApplyUsageGoal(user *models.User, usageGoal string) {
	if user == nil {
		return
	}
	user.UsageGoal = NormalizeUsageGoal(usageGoal)
}

// AlternativeUsageGoals lists the modes the owner is not currently in, in a
// stable order, so a quick-switch surface can offer every other mode without
// re-offering the one already in force. The order matches the chooser on every
// other surface: the neutral default first, the two alternative modes after it.
func AlternativeUsageGoals(current string) []string {
	normalized := NormalizeUsageGoal(current)
	alternatives := make([]string, 0, 2)
	for _, goal := range []string{models.UsageGoalHealth, models.UsageGoalAvoid, models.UsageGoalTrying} {
		if goal != normalized {
			alternatives = append(alternatives, goal)
		}
	}
	return alternatives
}

// SettingsCycleStartWindowDays is how far back the settings form accepts a
// cycle start, in days before today. A year of history: correcting this value
// is the one route an owner has to the anchor every prediction is measured
// from, and a cycle that began months ago is exactly the case an irregular or
// paused history produces.
//
// The floor used to be 1 January of the current year, which is a calendar
// artefact and not a cycle one: on 2 January the form took two dates, and a
// cycle that began in December — the normal case in early January — could only
// be recorded as a later, wrong date. Onboarding's twin carried the same floor
// and became a rolling window (OnboardingDateBounds, 60 days, the number its
// copy states); this one is wider because it is a correction surface rather
// than a first-run one, and rolling for the same reason. The window only ever
// grew: every date the January floor accepted is inside it.
const SettingsCycleStartWindowDays = 365

// SettingsCycleStartDateBounds returns the window the settings form accepts for
// the last cycle start, which is also the window its date picker offers: the
// last SettingsCycleStartWindowDays days, ending today.
func SettingsCycleStartDateBounds(now time.Time, location *time.Location) (time.Time, time.Time) {
	if location == nil {
		location = time.UTC
	}
	today := DateAtLocation(now.In(location), location)
	return AddCalendarDays(today, -SettingsCycleStartWindowDays, location), today
}
