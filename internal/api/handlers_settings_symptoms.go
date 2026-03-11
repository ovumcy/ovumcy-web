package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) buildSettingsSymptomsSectionData(c *fiber.Ctx, user *models.User, state settingsSymptomSectionState) (fiber.Map, error) {
	viewData, err := handler.settingsViewService.BuildSettingsSymptomsViewData(user)
	if err != nil {
		return nil, err
	}

	statusLocalizer := func(source string) string {
		return localizedSettingsSymptomStatus(c, source)
	}
	errorLocalizer := func(source string) string {
		return localizedSettingsSymptomError(c, source)
	}

	return fiber.Map{
		"ActiveCustomSymptoms":   buildSettingsSymptomRows(viewData.ActiveCustomSymptoms, state.Row, statusLocalizer, errorLocalizer),
		"ArchivedCustomSymptoms": buildSettingsSymptomRows(viewData.ArchivedCustomSymptoms, state.Row, statusLocalizer, errorLocalizer),
		"HasCustomSymptoms":      viewData.HasCustomSymptoms,
		"HasArchivedSymptoms":    viewData.HasArchivedSymptoms,
		"SymptomStatusMessage":   statusLocalizer(state.SuccessStatus),
		"SymptomErrorMessage":    errorLocalizer(state.ErrorMessage),
		"SymptomDraftName":       sanitizeDraftName(state.Draft.Name),
		"SymptomDraftIcon":       defaultSymptomDraftIcon(state.Draft.Icon),
		"SymptomIconOptions":     buildSettingsSymptomIconOptions(state.Draft.Icon),
	}, nil
}

func (handler *Handler) respondSymptomMutationError(c *fiber.Ctx, user *models.User, spec APIErrorSpec, state settingsSymptomSectionState) error {
	if isHTMX(c) {
		if state.Row.SymptomID != 0 {
			state.Row.ErrorMessage = spec.Key
		} else {
			state.ErrorMessage = spec.Key
			if spec.Key == "symptom name is too long" {
				state.Draft.Name = ""
			}
		}
		data, err := handler.buildSettingsSymptomsSectionData(c, user, state)
		if err != nil {
			return handler.respondMappedError(c, settingsLoadErrorSpec())
		}
		c.Status(fiber.StatusOK)
		return handler.renderPartial(c, "settings_symptoms_section", data)
	}

	if !acceptsJSON(c) {
		handler.setFlashCookie(c, FlashPayload{SettingsError: spec.Key})
		return c.Redirect("/settings", fiber.StatusSeeOther)
	}
	return handler.respondMappedError(c, spec)
}

func (handler *Handler) respondSymptomMutationSuccess(c *fiber.Ctx, user *models.User, statusCode int, successStatus string, state settingsSymptomSectionState) error {
	if isHTMX(c) {
		if state.Row.SymptomID != 0 {
			state.Row.SuccessStatus = successStatus
		} else {
			state.SuccessStatus = successStatus
		}
		data, err := handler.buildSettingsSymptomsSectionData(c, user, settingsSymptomSectionState{
			SuccessStatus: state.SuccessStatus,
			Row:           state.Row,
		})
		if err != nil {
			return handler.respondMappedError(c, settingsLoadErrorSpec())
		}
		c.Status(fiber.StatusOK)
		return handler.renderPartial(c, "settings_symptoms_section", data)
	}

	if !acceptsJSON(c) {
		handler.setFlashCookie(c, FlashPayload{SettingsSuccess: successStatus})
		return c.Redirect("/settings", fiber.StatusSeeOther)
	}

	return c.SendStatus(statusCode)
}

func localizedSettingsSymptomError(c *fiber.Ctx, source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}

	messages := currentMessages(c)
	if key := services.AuthErrorTranslationKey(source); key != "" {
		if localized := translateMessage(messages, key); localized != key {
			return localized
		}
	}
	return source
}

func localizedSettingsSymptomStatus(c *fiber.Ctx, status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return ""
	}

	messages := currentMessages(c)
	if key := services.SettingsStatusTranslationKey(status); key != "" {
		if localized := translateMessage(messages, key); localized != key {
			return localized
		}
	}
	return status
}

func defaultSymptomDraftIcon(raw string) string {
	icon := strings.TrimSpace(raw)
	if icon == "" {
		return "✨"
	}
	return icon
}
