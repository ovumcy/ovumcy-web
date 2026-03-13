package api

import (
	"time"

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
		spec := unauthorizedErrorSpec()
		handler.logHealthDataMutationError(c, "health.symptom_create", spec, "symptom")
		return handler.respondMappedError(c, spec)
	}

	payload := symptomPayload{}
	if err := c.BodyParser(&payload); err != nil {
		spec := settingsInvalidInputErrorSpec()
		handler.logHealthDataMutationError(c, "health.symptom_create", spec, "symptom")
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Draft: payload,
		})
	}

	symptom, err := handler.symptomService.CreateSymptomForUser(user.ID, payload.Name, payload.Icon, payload.Color)
	if err != nil {
		spec := mapSymptomCreateError(err)
		handler.logHealthDataMutationError(c, "health.symptom_create", spec, "symptom")
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Draft: payload,
		})
	}

	handler.logHealthDataMutation(c, "health.symptom_create", "success", "symptom")

	if acceptsJSON(c) {
		return c.Status(fiber.StatusCreated).JSON(symptom)
	}
	return handler.respondSymptomMutationSuccess(c, user, fiber.StatusCreated, "symptom_created", settingsSymptomSectionState{})
}

func (handler *Handler) UpdateSymptom(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logHealthDataMutationError(c, "health.symptom_update", spec, "symptom")
		return handler.respondMappedError(c, spec)
	}

	id, err := parseRequestUint(c.Params("id"))
	if err != nil {
		spec := invalidSymptomIDErrorSpec()
		handler.logHealthDataMutationError(c, "health.symptom_update", spec, "symptom")
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{})
	}

	payload := symptomPayload{}
	if err := c.BodyParser(&payload); err != nil {
		spec := settingsInvalidInputErrorSpec()
		handler.logHealthDataMutationError(c, "health.symptom_update", spec, "symptom")
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Row: settingsSymptomRowState{
				SymptomID:      id,
				Draft:          payload,
				UseDraftValues: true,
			},
		})
	}

	symptom, err := handler.symptomService.UpdateSymptomForUser(user.ID, id, payload.Name, payload.Icon, payload.Color)
	if err != nil {
		useDraftValues := true
		spec := mapSymptomUpdateError(err)
		if spec.Key == "symptom name is too long" {
			useDraftValues = false
		}
		handler.logHealthDataMutationError(c, "health.symptom_update", spec, "symptom")
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Row: settingsSymptomRowState{
				SymptomID:      id,
				Draft:          payload,
				UseDraftValues: useDraftValues,
			},
		})
	}

	handler.logHealthDataMutation(c, "health.symptom_update", "success", "symptom")

	if acceptsJSON(c) {
		return c.JSON(symptom)
	}
	return handler.respondSymptomMutationSuccess(c, user, fiber.StatusOK, "symptom_updated", settingsSymptomSectionState{
		Row: settingsSymptomRowState{SymptomID: id},
	})
}

func (handler *Handler) ArchiveSymptom(c *fiber.Ctx) error {
	return handler.archiveSymptom(c)
}

func (handler *Handler) DeleteSymptom(c *fiber.Ctx) error {
	return handler.archiveSymptom(c)
}

func (handler *Handler) RestoreSymptom(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logHealthDataMutationError(c, "health.symptom_restore", spec, "symptom")
		return handler.respondMappedError(c, spec)
	}

	id, err := parseRequestUint(c.Params("id"))
	if err != nil {
		spec := invalidSymptomIDErrorSpec()
		handler.logHealthDataMutationError(c, "health.symptom_restore", spec, "symptom")
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{})
	}
	if err := handler.symptomService.RestoreSymptomForUser(user.ID, id); err != nil {
		spec := mapSymptomRestoreError(err)
		handler.logHealthDataMutationError(c, "health.symptom_restore", spec, "symptom")
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Row: settingsSymptomRowState{SymptomID: id},
		})
	}

	handler.logHealthDataMutation(c, "health.symptom_restore", "success", "symptom")

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return handler.respondSymptomMutationSuccess(c, user, fiber.StatusOK, "symptom_restored", settingsSymptomSectionState{
		Row: settingsSymptomRowState{SymptomID: id},
	})
}

func (handler *Handler) archiveSymptom(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logHealthDataMutationError(c, "health.symptom_archive", spec, "symptom")
		return handler.respondMappedError(c, spec)
	}

	id, err := parseRequestUint(c.Params("id"))
	if err != nil {
		spec := invalidSymptomIDErrorSpec()
		handler.logHealthDataMutationError(c, "health.symptom_archive", spec, "symptom")
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{})
	}
	if err := handler.symptomService.ArchiveSymptomForUser(user.ID, id, time.Now()); err != nil {
		spec := mapSymptomArchiveError(err)
		handler.logHealthDataMutationError(c, "health.symptom_archive", spec, "symptom")
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Row: settingsSymptomRowState{SymptomID: id},
		})
	}

	handler.logHealthDataMutation(c, "health.symptom_archive", "success", "symptom")

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return handler.respondSymptomMutationSuccess(c, user, fiber.StatusOK, "symptom_hidden", settingsSymptomSectionState{
		Row: settingsSymptomRowState{SymptomID: id},
	})
}
