package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func parseDayPayload(c *fiber.Ctx) (dayPayload, error) {
	payload := dayPayload{Flow: models.FlowNone, SymptomIDs: []uint{}}
	contentType := strings.ToLower(c.Get("Content-Type"))

	if strings.Contains(contentType, "application/json") {
		if err := c.BodyParser(&payload); err != nil {
			return payload, err
		}
	} else {
		payload.IsPeriod = services.ParseBoolLike(c.FormValue("is_period"))
		payload.Flow = strings.ToLower(strings.TrimSpace(c.FormValue("flow")))
		payload.Notes = strings.TrimSpace(c.FormValue("notes"))

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
	payload.Notes = strings.TrimSpace(payload.Notes)

	return payload, nil
}
