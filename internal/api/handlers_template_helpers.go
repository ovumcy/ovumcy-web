package api

import (
	"html/template"

	"github.com/terraincognita07/ovumcy/internal/services"
)

func newTemplateFuncMap() template.FuncMap {
	return template.FuncMap{
		"formatDate":          formatTemplateDate,
		"formatLocalizedDate": formatTemplateLocalizedDate,
		"formatFloat":         formatTemplateFloat,
		"t": func(messages map[string]string, key string) string {
			return translateMessage(messages, key)
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
		"userIdentity":   templateUserIdentity,
		"hasDisplayName": templateHasDisplayName,
		"isActiveRoute":  isActiveTemplateRoute,
		"hasSymptom":     hasTemplateSymptom,
		"toJSON":         templateToJSON,
		"dict":           templateDict,
	}
}
