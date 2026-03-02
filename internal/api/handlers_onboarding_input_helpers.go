package api

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) parseOnboardingStep1Values(c *fiber.Ctx, today time.Time) (onboardingStep1Values, string) {
	input := onboardingStep1Input{}
	if err := c.BodyParser(&input); err != nil {
		return onboardingStep1Values{}, "invalid input"
	}
	parsedDay, err := handler.onboardingSvc.ValidateAndParseStep1StartDate(input.LastPeriodStart, today, handler.location)
	if err != nil {
		switch services.ClassifyOnboardingStep1Error(err) {
		case services.OnboardingStep1ErrorDateRequired:
			return onboardingStep1Values{}, "date is required"
		case services.OnboardingStep1ErrorDateInvalid:
			return onboardingStep1Values{}, "invalid last period start"
		case services.OnboardingStep1ErrorDateOutOfRange:
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

	contentType := strings.ToLower(c.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		if err := c.BodyParser(&input); err != nil {
			return onboardingStep2Input{}, "invalid input"
		}
	} else {
		input = onboardingStep2Input{
			CycleLength:    0,
			PeriodLength:   0,
			AutoPeriodFill: services.ParseBoolLike(c.FormValue("auto_period_fill")),
		}
		cycleLength, periodLength, autoPeriodFill, err := handler.onboardingSvc.ParseAndNormalizeStep2Input(
			c.FormValue("cycle_length"),
			c.FormValue("period_length"),
			input.AutoPeriodFill,
		)
		if err != nil {
			return onboardingStep2Input{}, "invalid input"
		}
		input.CycleLength = cycleLength
		input.PeriodLength = periodLength
		input.AutoPeriodFill = autoPeriodFill
		return input, ""
	}
	cycleLength, periodLength, autoPeriodFill, err := handler.onboardingSvc.ParseAndNormalizeStep2Input(
		strconv.Itoa(input.CycleLength),
		strconv.Itoa(input.PeriodLength),
		input.AutoPeriodFill,
	)
	if err != nil {
		return onboardingStep2Input{}, "invalid input"
	}
	input.CycleLength = cycleLength
	input.PeriodLength = periodLength
	input.AutoPeriodFill = autoPeriodFill

	return input, ""
}
