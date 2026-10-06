package api

import (
	"encoding/json"
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
		payload.IsPeriod = services.ParseBoolLike(formBodyValue(c, "is_period"))
		payload.ConfirmCycleStart = services.ParseBoolLike(formBodyValue(c, "cycle_start"))
		payload.PeriodFromStoredStart = services.ParseBoolLike(formBodyValue(c, "period_from_stored_start"))
		payload.Flow = strings.ToLower(strings.TrimSpace(formBodyValue(c, "flow")))
		payload.Mood, err = parseOptionalFormInt(formBodyValue(c, "mood"))
		if err != nil {
			return payload, err
		}
		if !hidden.SexActivity {
			payload.SexActivity = strings.ToLower(strings.TrimSpace(formBodyValue(c, "sex_activity")))
		}
		if !hidden.CervicalMucus {
			payload.CervicalMucus = strings.ToLower(strings.TrimSpace(formBodyValue(c, "cervical_mucus")))
		}
		payload.PregnancyTest = strings.ToLower(strings.TrimSpace(formBodyValue(c, "pregnancy_test")))
		if !hidden.Notes {
			payload.Notes = strings.TrimSpace(formBodyValue(c, "notes"))
		}
		if !hidden.BBT {
			payload.BBT, err = services.ParseDayBBTRawWithUnit(formBodyValue(c, "bbt"), temperatureUnit)
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

// dayPayloadPresence decodes which day fields a JSON body names. It is decoded
// by the same JSON decoder, with the same wire keys, as dayPayload itself, so a
// key the bind reads into a field is the key that marks the field present: a
// RawMessage is set for any value, an explicit null included, and stays nil
// only when the key is absent.
type dayPayloadPresence struct {
	IsPeriod        json.RawMessage `json:"is_period"`
	Flow            json.RawMessage `json:"flow"`
	Mood            json.RawMessage `json:"mood"`
	SexActivity     json.RawMessage `json:"sex_activity"`
	BBT             json.RawMessage `json:"bbt"`
	CervicalMucus   json.RawMessage `json:"cervical_mucus"`
	PregnancyTest   json.RawMessage `json:"pregnancy_test"`
	CycleFactorKeys json.RawMessage `json:"cycle_factor_keys"`
	SymptomIDs      json.RawMessage `json:"symptom_ids"`
	Notes           json.RawMessage `json:"notes"`
}

// parseDayPayloadFields reports which day fields a partial write names. A JSON
// body names the keys it carries; null is a stated value and clears the field.
// A form body names the fields it posts, read from the same sources
// parseDayPayload reads each one from; form encoding cannot tell an omitted
// field from an empty one, so a posted empty field clears it, and an unchecked
// checkbox, which posts nothing, changes nothing. A field the account hides is
// never named, for the reason parseDayPayload never reads it: the day form does
// not speak for it, so the stored value stands.
func parseDayPayloadFields(c fiber.Ctx, formBody bool, hidden preservedDayFields) (services.DayEntryFields, error) {
	if !formBody {
		var presence dayPayloadPresence
		// The bind in parseDayPayload has already decoded this body with the
		// same decoder into a stricter target, and a RawMessage refuses no
		// value that target accepted; the refusal is kept so that a decoder
		// change fails closed instead of reading every field as absent.
		// codecov:ignore:start
		if err := c.App().Config().JSONDecoder(c.Body(), &presence); err != nil {
			return services.DayEntryFields{}, err
		}
		// codecov:ignore:end
		return services.DayEntryFields{
			IsPeriod:        presence.IsPeriod != nil,
			Flow:            presence.Flow != nil,
			Mood:            presence.Mood != nil,
			SexActivity:     presence.SexActivity != nil,
			BBT:             presence.BBT != nil,
			CervicalMucus:   presence.CervicalMucus != nil,
			PregnancyTest:   presence.PregnancyTest != nil,
			CycleFactorKeys: presence.CycleFactorKeys != nil,
			SymptomIDs:      presence.SymptomIDs != nil,
			Notes:           presence.Notes != nil,
		}, nil
	}

	postArgs := c.RequestCtx().PostArgs()
	return services.DayEntryFields{
		IsPeriod:        formValuePresent(c, "is_period"),
		Flow:            formValuePresent(c, "flow"),
		Mood:            formValuePresent(c, "mood"),
		SexActivity:     !hidden.SexActivity && formValuePresent(c, "sex_activity"),
		BBT:             !hidden.BBT && formValuePresent(c, "bbt"),
		CervicalMucus:   !hidden.CervicalMucus && formValuePresent(c, "cervical_mucus"),
		PregnancyTest:   formValuePresent(c, "pregnancy_test"),
		CycleFactorKeys: !hidden.CycleFactors && postArgs.Has("cycle_factor_keys"),
		SymptomIDs:      postArgs.Has("symptom_ids"),
		Notes:           !hidden.Notes && formValuePresent(c, "notes"),
	}, nil
}

// formValuePresent reports whether key arrives in the form body: the
// urlencoded body or a multipart field, the two sources formBodyValue reads.
// The query string is never one — a day field the URL names is not part of the
// write, so it neither names a field nor supplies its value. The multipart form
// is fiber's own parse, bounded by the app's BodyLimit and already made by the
// formBodyValue reads before this runs — fasthttp's would parse again under
// its own default limit.
func formValuePresent(c fiber.Ctx, key string) bool {
	if c.RequestCtx().PostArgs().Has(key) {
		return true
	}
	form, err := c.MultipartForm()
	if err != nil {
		return false
	}
	_, present := form.Value[key]
	return present
}

// formBodyValue reads key from the form body only: the urlencoded body, then a
// multipart field. c.FormValue consults the query string before the body, so a
// field the URL names would stand in for one the body leaves out — or override
// one it posts.
func formBodyValue(c fiber.Ctx, key string) string {
	if args := c.RequestCtx().PostArgs(); args.Has(key) {
		return string(args.Peek(key))
	}
	form, err := c.MultipartForm()
	if err != nil {
		return ""
	}
	if values := form.Value[key]; len(values) > 0 {
		return values[0]
	}
	return ""
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
