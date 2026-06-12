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
	symptoms, err := handler.symptomService.FetchSymptoms(c.UserContext(), user.ID)
	if err != nil {
		return handler.respondMappedError(c, symptomsFetchErrorSpec())
	}
	return c.JSON(newSymptomResponses(symptoms))
}

var (
	symptomCreateMutation  = healthMutationKind{action: "health.symptom_create", target: "symptom"}
	symptomUpdateMutation  = healthMutationKind{action: "health.symptom_update", target: "symptom"}
	symptomRestoreMutation = healthMutationKind{action: "health.symptom_restore", target: "symptom"}
	symptomArchiveMutation = healthMutationKind{action: "health.symptom_archive", target: "symptom"}
)

func (handler *Handler) CreateSymptom(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, symptomCreateMutation, unauthorizedErrorSpec())
	}

	payload := symptomPayload{}
	if err := c.BodyParser(&payload); err != nil {
		spec := settingsInvalidInputErrorSpec()
		handler.logMutationError(c, symptomCreateMutation, spec)
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Draft: payload,
		})
	}

	symptom, err := handler.symptomService.CreateSymptomForUser(c.UserContext(), user.ID, payload.Name, payload.Icon, payload.Color)
	if err != nil {
		spec := mapSymptomCreateError(err)
		handler.logMutationError(c, symptomCreateMutation, spec)
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Draft: payload,
		})
	}

	handler.logMutationSuccess(c, symptomCreateMutation)

	if acceptsJSON(c) {
		return c.Status(fiber.StatusCreated).JSON(newSymptomResponse(symptom))
	}
	return handler.respondSymptomMutationSuccess(c, user, fiber.StatusCreated, "symptom_created", settingsSymptomSectionState{})
}

func (handler *Handler) UpdateSymptom(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, symptomUpdateMutation, unauthorizedErrorSpec())
	}

	id, err := parseRequestUint(c.Params("id"))
	if err != nil {
		spec := invalidSymptomIDErrorSpec()
		handler.logMutationError(c, symptomUpdateMutation, spec)
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{})
	}

	payload := symptomPayload{}
	if err := c.BodyParser(&payload); err != nil {
		spec := settingsInvalidInputErrorSpec()
		handler.logMutationError(c, symptomUpdateMutation, spec)
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Row: settingsSymptomRowState{
				SymptomID:      id,
				Draft:          payload,
				UseDraftValues: true,
			},
		})
	}

	symptom, err := handler.symptomService.UpdateSymptomForUser(c.UserContext(), user.ID, id, payload.Name, payload.Icon, payload.Color)
	if err != nil {
		useDraftValues := true
		spec := mapSymptomUpdateError(err)
		if spec.Key == "symptom name is too long" {
			useDraftValues = false
		}
		handler.logMutationError(c, symptomUpdateMutation, spec)
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Row: settingsSymptomRowState{
				SymptomID:      id,
				Draft:          payload,
				UseDraftValues: useDraftValues,
			},
		})
	}

	handler.logMutationSuccess(c, symptomUpdateMutation)

	if acceptsJSON(c) {
		return c.JSON(newSymptomResponse(symptom))
	}
	return handler.respondSymptomMutationSuccess(c, user, fiber.StatusOK, "symptom_updated", settingsSymptomSectionState{
		Row: settingsSymptomRowState{SymptomID: id},
	})
}

// DeleteSymptom intentionally archives instead of erasing: the symptom is
// restorable from settings and its name persists until clear-data or
// account deletion (documented in docs/gdpr.md under Rectification).
func (handler *Handler) DeleteSymptom(c *fiber.Ctx) error {
	return handler.archiveSymptom(c)
}

func (handler *Handler) RestoreSymptom(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, symptomRestoreMutation, unauthorizedErrorSpec())
	}

	id, err := parseRequestUint(c.Params("id"))
	if err != nil {
		spec := invalidSymptomIDErrorSpec()
		handler.logMutationError(c, symptomRestoreMutation, spec)
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{})
	}
	if err := handler.symptomService.RestoreSymptomForUser(c.UserContext(), user.ID, id); err != nil {
		spec := mapSymptomRestoreError(err)
		handler.logMutationError(c, symptomRestoreMutation, spec)
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Row: settingsSymptomRowState{SymptomID: id},
		})
	}

	handler.logMutationSuccess(c, symptomRestoreMutation)

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
		return handler.failMutation(c, symptomArchiveMutation, unauthorizedErrorSpec())
	}

	id, err := parseRequestUint(c.Params("id"))
	if err != nil {
		spec := invalidSymptomIDErrorSpec()
		handler.logMutationError(c, symptomArchiveMutation, spec)
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{})
	}
	if err := handler.symptomService.ArchiveSymptomForUser(c.UserContext(), user.ID, id, time.Now()); err != nil {
		spec := mapSymptomArchiveError(err)
		handler.logMutationError(c, symptomArchiveMutation, spec)
		return handler.respondSymptomMutationError(c, user, spec, settingsSymptomSectionState{
			Row: settingsSymptomRowState{SymptomID: id},
		})
	}

	handler.logMutationSuccess(c, symptomArchiveMutation)

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return handler.respondSymptomMutationSuccess(c, user, fiber.StatusOK, "symptom_hidden", settingsSymptomSectionState{
		Row: settingsSymptomRowState{SymptomID: id},
	})
}
