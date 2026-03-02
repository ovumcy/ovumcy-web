package api

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) GetSymptoms(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}
	symptoms, err := handler.symptomService.FetchSymptoms(user.ID)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to fetch symptoms")
	}
	return c.JSON(symptoms)
}

func (handler *Handler) CreateSymptom(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	payload := symptomPayload{}
	if err := c.BodyParser(&payload); err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid payload")
	}
	symptom, err := handler.symptomService.CreateSymptomForUser(user.ID, payload.Name, payload.Icon, payload.Color)
	if err != nil {
		switch services.ClassifySymptomCreateError(err) {
		case services.SymptomCreateErrorInvalidName:
			return apiError(c, fiber.StatusBadRequest, "invalid symptom name")
		case services.SymptomCreateErrorInvalidColor:
			return apiError(c, fiber.StatusBadRequest, "invalid symptom color")
		case services.SymptomCreateErrorFailed:
			return apiError(c, fiber.StatusInternalServerError, "failed to create symptom")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to create symptom")
		}
	}
	return c.Status(fiber.StatusCreated).JSON(symptom)
}

func (handler *Handler) DeleteSymptom(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid symptom id")
	}
	if err := handler.symptomService.DeleteSymptomForUser(user.ID, uint(id)); err != nil {
		switch services.ClassifySymptomDeleteError(err) {
		case services.SymptomDeleteErrorNotFound:
			return apiError(c, fiber.StatusNotFound, "symptom not found")
		case services.SymptomDeleteErrorBuiltinForbidden:
			return apiError(c, fiber.StatusBadRequest, "built-in symptom cannot be deleted")
		case services.SymptomDeleteErrorDeleteFailed:
			return apiError(c, fiber.StatusInternalServerError, "failed to delete symptom")
		case services.SymptomDeleteErrorCleanLogsFailed:
			return apiError(c, fiber.StatusInternalServerError, "failed to clean symptom logs")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to delete symptom")
		}
	}

	return c.JSON(fiber.Map{"ok": true})
}
