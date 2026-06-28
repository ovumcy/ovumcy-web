package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func parseDayPayload(c *fiber.Ctx, user *models.User) (dayPayload, error) {
	payload := dayPayload{Flow: models.FlowNone, SymptomIDs: []uint{}}
	temperatureUnit := services.DefaultTemperatureUnit
	if user != nil {
		temperatureUnit = user.TemperatureUnit
	}

	if hasJSONBody(c) {
		if err := c.BodyParser(&payload); err != nil {
			return payload, err
		}
	} else {
		var err error
		payload.IsPeriod = services.ParseBoolLike(c.FormValue("is_period"))
		payload.Flow = strings.ToLower(strings.TrimSpace(c.FormValue("flow")))
		payload.Mood = clampFormIntValue(c.FormValue("mood"))
		payload.SexActivity = strings.ToLower(strings.TrimSpace(c.FormValue("sex_activity")))
		payload.CervicalMucus = strings.ToLower(strings.TrimSpace(c.FormValue("cervical_mucus")))
		payload.PregnancyTest = strings.ToLower(strings.TrimSpace(c.FormValue("pregnancy_test")))
		payload.LHTest = strings.ToLower(strings.TrimSpace(c.FormValue("lh_test")))
		payload.Notes = strings.TrimSpace(c.FormValue("notes"))
		payload.BBT, err = services.ParseDayBBTRawWithUnit(c.FormValue("bbt"), temperatureUnit)
		if err != nil {
			return payload, err
		}

		symptomRaw := c.Context().PostArgs().PeekMulti("symptom_ids")
		for _, value := range symptomRaw {
			parsed, err := parseRequestUint(string(value))
			if err == nil {
				payload.SymptomIDs = append(payload.SymptomIDs, parsed)
			}
		}

		cycleFactorRaw := c.Context().PostArgs().PeekMulti("cycle_factor_keys")
		for _, value := range cycleFactorRaw {
			payload.CycleFactorKeys = append(payload.CycleFactorKeys, string(value))
		}
	}

	payload.Flow = strings.ToLower(strings.TrimSpace(payload.Flow))
	if payload.Flow == "" {
		payload.Flow = models.FlowNone
	}
	payload.SexActivity = services.NormalizeDaySexActivity(payload.SexActivity)
	payload.CervicalMucus = services.NormalizeDayCervicalMucus(payload.CervicalMucus)
	payload.PregnancyTest = services.NormalizeDayPregnancyTest(payload.PregnancyTest)
	payload.LHTest = services.NormalizeDayLHTest(payload.LHTest)
	payload.Notes = strings.TrimSpace(payload.Notes)

	return payload, nil
}

func clampFormIntValue(raw string) int {
	value, err := parseRequestInt(raw)
	if err != nil {
		return 0
	}
	return value
}
