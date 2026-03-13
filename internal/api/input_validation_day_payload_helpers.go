package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
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
	}

	payload.Flow = strings.ToLower(strings.TrimSpace(payload.Flow))
	if payload.Flow == "" {
		payload.Flow = models.FlowNone
	}
	payload.SexActivity = services.NormalizeDaySexActivity(payload.SexActivity)
	payload.CervicalMucus = services.NormalizeDayCervicalMucus(payload.CervicalMucus)
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
