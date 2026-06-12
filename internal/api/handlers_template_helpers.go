package api

import (
	"html/template"

	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func newTemplateFuncMap() template.FuncMap {
	return template.FuncMap{
		"formatLocalizedDate": formatTemplateLocalizedDate,
		"formatFloat":         formatTemplateFloat,
		"t": func(messages map[string]string, key string) string {
			return translateMessage(messages, key)
		},
		"tn": func(messages map[string]string, language string, key string, n int) string {
			return i18n.TranslatePlural(messages, language, key, n)
		},
		"phaseLabel": func(messages map[string]string, phase string) string {
			return translateMessage(messages, services.PhaseTranslationKey(phase))
		},
		"phaseIcon": services.PhaseIcon,
		"flowLabel": func(messages map[string]string, flow string) string {
			return translateMessage(messages, services.FlowTranslationKey(flow))
		},
		"sexActivityLabel": func(messages map[string]string, value string) string {
			return translateMessage(messages, services.SexActivityTranslationKey(value))
		},
		"cervicalMucusLabel": func(messages map[string]string, value string) string {
			return translateMessage(messages, services.CervicalMucusTranslationKey(value))
		},
		"pregnancyTestLabel": func(messages map[string]string, value string) string {
			return translateMessage(messages, services.PregnancyTestTranslationKey(value))
		},
		"cycleFactorLabel": func(messages map[string]string, key string) string {
			translationKey := services.DayCycleFactorTranslationKey(key)
			if translationKey == "" {
				return key
			}
			return translateMessage(messages, translationKey)
		},
		"cycleFactorIcon": services.DayCycleFactorIcon,
		"symptomLabel": func(messages map[string]string, name string) string {
			key := services.BuiltinSymptomTranslationKey(name)
			if key == "" {
				return name
			}
			return translateMessage(messages, key)
		},
		"symptomGroup": services.SymptomGroup,
		"roleLabel": func(messages map[string]string, role string) string {
			return translateMessage(messages, services.RoleTranslationKey(role))
		},
		"moodEmoji": services.MoodEmoji,
		"hasBBT": func(value float64) bool {
			return services.IsValidDayBBT(value) && value > 0
		},
		"displayBBT": func(value float64, unit string) string {
			return services.FormatDayBBTForInput(value, unit)
		},
		"temperatureUnitSymbol": services.TemperatureUnitSymbol,
		"userIdentity":          templateUserIdentity,
		"hasDisplayName":        templateHasDisplayName,
		"isActiveRoute":         isActiveTemplateRoute,
		"hasSymptom":            hasTemplateSymptom,
		"hasCycleFactor":        hasTemplateCycleFactor,
		"statusOK": func(message string) template.HTML {
			return httpx.StatusOKTemplateHTML(message)
		},
		"dismissibleStatusOK": func(messages map[string]string, message string) template.HTML {
			return httpx.DismissibleStatusOKTemplateHTML(message, localizedStatusDismissLabel(messages))
		},
		"toJSON": templateToJSON,
		"dict":   templateDict,
	}
}

func hasTemplateCycleFactor(selected map[string]bool, key string) bool {
	if len(selected) == 0 {
		return false
	}
	return selected[key]
}
