package api

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const onboardingTimezoneFieldName = "client_timezone"

func onboardingFormTimezoneValue(c *fiber.Ctx) string {
	raw := strings.TrimSpace(string(c.Request().PostArgs().Peek(onboardingTimezoneFieldName)))
	if raw != "" {
		return raw
	}

	values, err := url.ParseQuery(string(c.Body()))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(values.Get(onboardingTimezoneFieldName))
}

func (handler *Handler) requestLocationFromOnboardingForm(c *fiber.Ctx) *time.Location {
	if location, canonical, ok := parseRequestTimezone(c.Get(timezoneHeaderName)); ok {
		if strings.TrimSpace(c.Cookies(timezoneCookieName)) != canonical {
			handler.setTimezoneCookie(c, canonical)
		}
		return location
	}

	if location, _, ok := parseRequestTimezone(c.Cookies(timezoneCookieName)); ok {
		return location
	}

	rawTimezone := onboardingFormTimezoneValue(c)
	if location, canonical, ok := parseRequestTimezone(rawTimezone); ok {
		handler.setTimezoneCookie(c, canonical)
		return location
	}

	return handler.requestLocation(c)
}

func (handler *Handler) parseOnboardingStep1Values(c *fiber.Ctx, today time.Time, location *time.Location) (onboardingStep1Values, string) {
	input := onboardingStep1Input{}
	if err := c.BodyParser(&input); err != nil {
		return onboardingStep1Values{}, "invalid input"
	}
	parsedDay, err := handler.onboardingSvc.ValidateAndParseStep1StartDate(input.LastPeriodStart, today, location)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrOnboardingStartDateRequired):
			return onboardingStep1Values{}, "date is required"
		case errors.Is(err, services.ErrOnboardingStartDateInvalid):
			return onboardingStep1Values{}, "invalid last period start"
		case errors.Is(err, services.ErrOnboardingStartDateOutOfRange):
			return onboardingStep1Values{}, "last period start must be within last 60 days"
		default:
			return onboardingStep1Values{}, "last period start must be within last 60 days"
		}
	}

	return onboardingStep1Values{
		Start: parsedDay,
	}, ""
}

func (handler *Handler) parseOnboardingStep2Input(c *fiber.Ctx) (onboardingStep2Input, string) {
	input := onboardingStep2Input{}

	if hasJSONBody(c) {
		if err := c.BodyParser(&input); err != nil {
			return onboardingStep2Input{}, "invalid input"
		}
	} else {
		input = onboardingStep2Input{
			CycleLength:    0,
			PeriodLength:   0,
			AutoPeriodFill: services.ParseBoolLike(c.FormValue("auto_period_fill")),
			IrregularCycle: services.ParseBoolLike(c.FormValue("irregular_cycle")),
			AgeGroup:       strings.TrimSpace(c.FormValue("age_group")),
			UsageGoal:      strings.TrimSpace(c.FormValue("usage_goal")),
		}
		cycleLength, periodLength, autoPeriodFill, irregularCycle, ageGroup, usageGoal, err := handler.onboardingSvc.ParseAndNormalizeStep2Input(
			c.FormValue("cycle_length"),
			c.FormValue("period_length"),
			input.AutoPeriodFill,
			input.IrregularCycle,
			input.AgeGroup,
			input.UsageGoal,
		)
		if err != nil {
			return onboardingStep2Input{}, "invalid input"
		}
		input.CycleLength = cycleLength
		input.PeriodLength = periodLength
		input.AutoPeriodFill = autoPeriodFill
		input.IrregularCycle = irregularCycle
		input.AgeGroup = ageGroup
		input.UsageGoal = usageGoal
		return input, ""
	}
	cycleLength, periodLength, autoPeriodFill, irregularCycle, ageGroup, usageGoal, err := handler.onboardingSvc.ParseAndNormalizeStep2Input(
		strconv.Itoa(input.CycleLength),
		strconv.Itoa(input.PeriodLength),
		input.AutoPeriodFill,
		input.IrregularCycle,
		input.AgeGroup,
		input.UsageGoal,
	)
	if err != nil {
		return onboardingStep2Input{}, "invalid input"
	}
	input.CycleLength = cycleLength
	input.PeriodLength = periodLength
	input.AutoPeriodFill = autoPeriodFill
	input.IrregularCycle = irregularCycle
	input.AgeGroup = ageGroup
	input.UsageGoal = usageGoal

	return input, ""
}
