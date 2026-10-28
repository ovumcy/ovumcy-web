package api

import (
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type settingsSymptomIconOption struct {
	Value    string
	Selected bool
	IsCustom bool
}

type settingsSymptomRowView struct {
	Symptom        models.SymptomType
	FormName       string
	FormIcon       string
	IconOptions    []settingsSymptomIconOption
	ErrorMessage   string
	SuccessMessage string
}

type settingsSymptomRowState struct {
	SymptomID      uint
	SuccessStatus  string
	ErrorMessage   string
	Draft          symptomPayload
	UseDraftValues bool
}

type settingsSymptomSectionState struct {
	SuccessStatus string
	ErrorMessage  string
	Draft         symptomPayload
	Row           settingsSymptomRowState
}

var settingsSymptomIconCatalog = []string{
	"✨",
	"🔥",
	"💧",
	"⚡",
	"🌙",
	"🤕",
	"🌀",
	"🍫",
}

func buildSettingsSymptomRows(symptoms []models.SymptomType, rowState settingsSymptomRowState, statusLocalizer func(string) string, errorLocalizer func(string) string) []settingsSymptomRowView {
	rows := make([]settingsSymptomRowView, 0, len(symptoms))
	for _, symptom := range symptoms {
		useDraft := rowState.SymptomID != 0 && rowState.SymptomID == symptom.ID && rowState.UseDraftValues
		formName := symptom.Name
		formIcon := symptom.Icon
		if useDraft {
			formName = sanitizeDraftName(rowState.Draft.Name)
			formIcon = defaultSymptomDraftIcon(rowState.Draft.Icon)
		}

		row := settingsSymptomRowView{
			Symptom:     symptom,
			FormName:    formName,
			FormIcon:    formIcon,
			IconOptions: buildSettingsSymptomIconOptions(formIcon),
		}

		if rowState.SymptomID == symptom.ID {
			row.ErrorMessage = errorLocalizer(rowState.ErrorMessage)
			row.SuccessMessage = statusLocalizer(rowState.SuccessStatus)
		}

		rows = append(rows, row)
	}

	return rows
}

func settingsSymptomRowsInclude(rows []settingsSymptomRowView, symptomID uint) bool {
	for _, row := range rows {
		if row.Symptom.ID == symptomID {
			return true
		}
	}
	return false
}

func buildSettingsSymptomIconOptions(current string) []settingsSymptomIconOption {
	selected := defaultSymptomDraftIcon(current)
	options := make([]settingsSymptomIconOption, 0, len(settingsSymptomIconCatalog)+1)
	if !settingsSymptomIconInCatalog(selected) {
		options = append(options, settingsSymptomIconOption{
			Value:    selected,
			Selected: true,
			IsCustom: true,
		})
	}
	for _, value := range settingsSymptomIconCatalog {
		options = append(options, settingsSymptomIconOption{
			Value:    value,
			Selected: value == selected,
		})
	}
	return options
}

func settingsSymptomIconInCatalog(value string) bool {
	for _, option := range settingsSymptomIconCatalog {
		if option == value {
			return true
		}
	}
	return false
}

func sanitizeDraftName(raw string) string {
	return strings.ToValidUTF8(raw, "")
}
