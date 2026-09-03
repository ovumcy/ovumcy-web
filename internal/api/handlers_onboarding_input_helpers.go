package api

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const onboardingTimezoneFieldName = "client_timezone"

func onboardingFormTimezoneValue(c fiber.Ctx) string {
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

func (handler *Handler) requestLocationFromOnboardingForm(c fiber.Ctx) *time.Location {
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

func (handler *Handler) parseOnboardingStep1Values(c fiber.Ctx, today time.Time, location *time.Location) (onboardingStep1Values, string) {
	input := onboardingStep1Input{}
	if err := c.Bind().Body(&input); err != nil {
		return onboardingStep1Values{}, "invalid input"
	}
	parsedDay, err := handler.onboardingSvc.ValidateAndParseStep1StartDate(input.LastPeriodStart, today, location)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrOnboardingStartDateRequired):
			return onboardingStep1Values{}, "date is required"
		case errors.Is(err, services.ErrOnboardingStartDateInvalid):
			return onboardingStep1Values{}, "invalid last period start"
		// The out-of-range sentinel and an unrecognized error share the
		// default: the range is the last thing step 1 validates, so any
		// remaining failure is answered as the range refusal.
		default:
			return onboardingStep1Values{}, "last period start must be within last 60 days"
		}
	}

	return onboardingStep1Values{
		Start: parsedDay,
	}, ""
}

// onboardingStep2CarriesRemovedAgeGroup reports whether the step-2 request
// still names `age_group`, which v1.9.2 collected here. Onboarding stopped
// asking for the age bracket; the field is written by
// PATCH /api/v1/users/current/cycle only. Without this refusal a client
// written against the old contract is answered 200 while the value it
// submitted is dropped — the removal reads as a successful save, which is the
// one answer a removed field must never give.
//
// Presence is decided from the raw request, never from whether a typed decode
// of the value succeeded: a JSON number or `null` for `age_group` fails to
// bind into a `*string` probe with a TYPE error, which reads identically to
// the key being absent unless the two are told apart deliberately. The URL's
// own query string belongs to neither transport — fiber's `FormValue` reads
// it ahead of a form body, and nothing stops a client from attaching it to a
// JSON request either — so it is checked once, unconditionally, before
// branching on Content-Type; checking it only on the non-JSON arm left a JSON
// request with the field in its query string free to drop it silently. Both
// transports are then asked for the body itself, in their own spelling, so
// the two cannot diverge.
func onboardingStep2CarriesRemovedAgeGroup(c fiber.Ctx) bool {
	if c.Request().URI().QueryArgs().Has("age_group") {
		return true
	}
	if hasJSONBody(c) {
		return jsonBodyNamesKey(c.Body(), "age_group")
	}
	return onboardingNonJSONBodyHasField(c, "age_group")
}

// jsonBodyNamesKey reports whether the given key is present in a JSON object
// body, independent of the value's own type — a present `null` or a present
// number both count, since the question is "did the client name this field",
// not "did it parse into the type we expect". The comparison is
// case-insensitive because that is what it is standing in for: `Bind().Body`
// matches a JSON object key to a struct field with a case-insensitive
// fallback when no exact match exists, so a body naming `Age_Group` or
// `AGE_GROUP` used to bind into the removed field, refused, exactly as
// `age_group` was — the population this guard exists for is clients still on
// the pre-v2.0.0 contract, precisely where off-spec casing lives. A body that
// is not a JSON object (including one too malformed to decode at all) reports
// the key absent; the subsequent `Bind().Body` call answers the generic
// "invalid input" for that case, so presence detection never needs to
// duplicate it.
func jsonBodyNamesKey(body []byte, key string) bool {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return false
	}
	for fieldName := range fields {
		if strings.EqualFold(fieldName, key) {
			return true
		}
	}
	return false
}

// onboardingNonJSONBodyHasField reports whether a non-JSON request's BODY
// carries the given field, in either of the two places fiber's own
// `FormValue` reads from besides the query string (already checked by the
// caller, ahead of the Content-Type branch): an
// `application/x-www-form-urlencoded` body, and a multipart form field.
// Checking `PostArgs` alone (fasthttp only populates it for an exact
// `application/x-www-form-urlencoded` Content-Type) misses every multipart
// submission outright.
func onboardingNonJSONBodyHasField(c fiber.Ctx, key string) bool {
	if c.Request().PostArgs().Has(key) {
		return true
	}
	if c.IsMultipart() {
		if form, err := c.MultipartForm(); err == nil {
			if _, present := form.Value[key]; present {
				return true
			}
		}
	}
	return false
}

func (handler *Handler) parseOnboardingStep2Input(c fiber.Ctx) (onboardingStep2Input, string) {
	input := onboardingStep2Input{}

	if onboardingStep2CarriesRemovedAgeGroup(c) {
		return onboardingStep2Input{}, "onboarding does not accept an age group"
	}

	if hasJSONBody(c) {
		if err := c.Bind().Body(&input); err != nil {
			return onboardingStep2Input{}, "invalid input"
		}
	} else {
		input = onboardingStep2Input{
			CycleLength:    0,
			PeriodLength:   0,
			AutoPeriodFill: services.ParseBoolLike(c.FormValue("auto_period_fill")),
			IrregularCycle: services.ParseBoolLike(c.FormValue("irregular_cycle")),
			UsageGoal:      strings.TrimSpace(c.FormValue("usage_goal")),
		}
		cycleLength, periodLength, autoPeriodFill, irregularCycle, usageGoal, err := handler.onboardingSvc.ParseAndNormalizeStep2Input(
			c.FormValue("cycle_length"),
			c.FormValue("period_length"),
			input.AutoPeriodFill,
			input.IrregularCycle,
			input.UsageGoal,
		)
		if err != nil {
			return onboardingStep2Input{}, "invalid input"
		}
		input.CycleLength = cycleLength
		input.PeriodLength = periodLength
		input.AutoPeriodFill = autoPeriodFill
		input.IrregularCycle = irregularCycle
		input.UsageGoal = usageGoal
		return input, ""
	}
	cycleLength, periodLength, autoPeriodFill, irregularCycle, usageGoal, err := handler.onboardingSvc.ParseAndNormalizeStep2Input(
		strconv.Itoa(input.CycleLength),
		strconv.Itoa(input.PeriodLength),
		input.AutoPeriodFill,
		input.IrregularCycle,
		input.UsageGoal,
	)
	if err != nil {
		return onboardingStep2Input{}, "invalid input"
	}
	input.CycleLength = cycleLength
	input.PeriodLength = periodLength
	input.AutoPeriodFill = autoPeriodFill
	input.IrregularCycle = irregularCycle
	input.UsageGoal = usageGoal

	return input, ""
}
