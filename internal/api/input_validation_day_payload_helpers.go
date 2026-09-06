package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// parseDayPayload reads one day write off the wire. A field named in hidden is
// not read at all in the form branch: the account keeps it out of the day form,
// so whatever arrives under that name is not this write's subject.
//
// Only for the temperature does the skip decide the day. Reading that field IS
// a parse, and a parse can refuse: "abc" or 1e400 answers an error right here,
// before the save could replace the field with the value already stored
// (mergePreservedDayEntryInput), and no range check downstream ever sees it.
// For the other four a read could at worst be neutralised one layer later
// (services.dropPreservedDayEntryFields), and they are skipped anyway so that
// "a hidden field is not read" stays one rule instead of a temperature
// exception every reader has to rediscover.
//
// formBody is the transport condition, resolved once by the caller; which
// fields are hidden, and why a JSON body has none: hiddenDayFields. Regression:
// TestParseDayPayloadSkipsEveryFieldTheAccountHides,
// TestUpsertDayDoesNotReadAHiddenTemperatureField.
func parseDayPayload(c fiber.Ctx, user *models.User, formBody bool, hidden preservedDayFields) (dayPayload, error) {
	payload := dayPayload{Flow: models.FlowNone, SymptomIDs: []uint{}}
	temperatureUnit := services.DefaultTemperatureUnit
	if user != nil {
		temperatureUnit = user.TemperatureUnit
	}

	if !formBody {
		if err := c.Bind().Body(&payload); err != nil {
			return payload, err
		}
		payload.BBT = services.ConvertDayBBTToStorage(payload.BBT, temperatureUnit)
	} else {
		var err error
		payload.IsPeriod = services.ParseBoolLike(c.FormValue("is_period"))
		payload.ConfirmCycleStart = services.ParseBoolLike(c.FormValue("cycle_start"))
		payload.Flow = strings.ToLower(strings.TrimSpace(c.FormValue("flow")))
		payload.Mood, err = parseOptionalFormInt(c.FormValue("mood"))
		if err != nil {
			return payload, err
		}
		if !hidden.SexActivity {
			payload.SexActivity = strings.ToLower(strings.TrimSpace(c.FormValue("sex_activity")))
		}
		if !hidden.CervicalMucus {
			payload.CervicalMucus = strings.ToLower(strings.TrimSpace(c.FormValue("cervical_mucus")))
		}
		payload.PregnancyTest = strings.ToLower(strings.TrimSpace(c.FormValue("pregnancy_test")))
		if !hidden.Notes {
			payload.Notes = strings.TrimSpace(c.FormValue("notes"))
		}
		if !hidden.BBT {
			payload.BBT, err = services.ParseDayBBTRawWithUnit(c.FormValue("bbt"), temperatureUnit)
			if err != nil {
				return payload, err
			}
		}

		// symptom_ids stays deliberately lenient: an unparseable member of a
		// multi-value is dropped rather than refused, which is the tolerance a
		// checkbox group is posted with. That is intent, not the oversight
		// mood carried — pinned by
		// TestParseDayPayloadIgnoresOutOfRangeSymptomIDs.
		symptomRaw := c.RequestCtx().PostArgs().PeekMulti("symptom_ids")
		for _, value := range symptomRaw {
			parsed, err := parseRequestUint(string(value))
			if err == nil {
				payload.SymptomIDs = append(payload.SymptomIDs, parsed)
			}
		}

		if !hidden.CycleFactors {
			cycleFactorRaw := c.RequestCtx().PostArgs().PeekMulti("cycle_factor_keys")
			for _, value := range cycleFactorRaw {
				payload.CycleFactorKeys = append(payload.CycleFactorKeys, string(value))
			}
		}
	}

	payload.Flow = strings.ToLower(strings.TrimSpace(payload.Flow))
	if payload.Flow == "" {
		payload.Flow = models.FlowNone
	}
	payload.SexActivity = services.NormalizeDaySexActivity(payload.SexActivity)
	payload.CervicalMucus = services.NormalizeDayCervicalMucus(payload.CervicalMucus)
	payload.PregnancyTest = services.NormalizeDayPregnancyTest(payload.PregnancyTest)
	payload.Notes = strings.TrimSpace(payload.Notes)

	return payload, nil
}

// parseOptionalFormInt reads a scalar integer a form may legitimately omit. An
// absent or empty field is the owner recording nothing (an unchecked radio
// posts no field at all), which is what the JSON branch expresses by leaving
// the key out; a value that is present and unparseable is an error, exactly as
// the JSON bind reports it. The name it replaces — clampFormIntValue — claimed
// a third behaviour it never had: it neither clamped nor rejected, it
// substituted zero, and for mood zero means "no mood recorded", so a malformed
// update erased a recorded value. Regression:
// TestParseDayPayloadTreatsAMalformedScalarTheSameInBothBranches.
func parseOptionalFormInt(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return parseRequestInt(raw)
}
