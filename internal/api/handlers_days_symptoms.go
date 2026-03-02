package api

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) GetSymptoms(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if user.Role != models.RoleOwner {
		return apiError(c, fiber.StatusForbidden, "owner access required")
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
		switch {
		case errors.Is(err, services.ErrInvalidSymptomName):
			return apiError(c, fiber.StatusBadRequest, "invalid symptom name")
		case errors.Is(err, services.ErrInvalidSymptomColor):
			return apiError(c, fiber.StatusBadRequest, "invalid symptom color")
		case errors.Is(err, services.ErrCreateSymptomFailed):
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
		switch {
		case errors.Is(err, services.ErrSymptomNotFound):
			return apiError(c, fiber.StatusNotFound, "symptom not found")
		case errors.Is(err, services.ErrBuiltinSymptomDeleteForbidden):
			return apiError(c, fiber.StatusBadRequest, "built-in symptom cannot be deleted")
		case errors.Is(err, services.ErrDeleteSymptomFailed):
			return apiError(c, fiber.StatusInternalServerError, "failed to delete symptom")
		case errors.Is(err, services.ErrCleanSymptomLogsFailed):
			return apiError(c, fiber.StatusInternalServerError, "failed to clean symptom logs")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to delete symptom")
		}
	}

	return c.JSON(fiber.Map{"ok": true})
}
