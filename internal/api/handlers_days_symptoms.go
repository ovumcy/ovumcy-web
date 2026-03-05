package api

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) GetSymptoms(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}
	symptoms, err := handler.symptomService.FetchSymptoms(user.ID)
	if err != nil {
		return handler.respondMappedError(c, symptomsFetchErrorSpec())
	}
	return c.JSON(symptoms)
}

func (handler *Handler) CreateSymptom(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	payload := symptomPayload{}
	if err := c.BodyParser(&payload); err != nil {
		return handler.respondMappedError(c, invalidPayloadErrorSpec())
	}
	symptom, err := handler.symptomService.CreateSymptomForUser(user.ID, payload.Name, payload.Icon, payload.Color)
	if err != nil {
		return handler.respondMappedError(c, mapSymptomCreateError(err))
	}
	return c.Status(fiber.StatusCreated).JSON(symptom)
}

func (handler *Handler) DeleteSymptom(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return handler.respondMappedError(c, invalidSymptomIDErrorSpec())
	}
	if err := handler.symptomService.DeleteSymptomForUser(user.ID, uint(id)); err != nil {
		return handler.respondMappedError(c, mapSymptomDeleteError(err))
	}

	return c.JSON(fiber.Map{"ok": true})
}
