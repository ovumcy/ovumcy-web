package services

import (
	"reflect"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type BuiltinSymptomMessages interface {
	SupportedLanguages() []string
	Messages(language string) map[string]string
}

func BuiltinSymptomTranslationKey(name string) string {
	if symptom, ok := builtinSymptomByName(name); ok {
		return symptom.TranslationKey
	}
	return ""
}

func BuiltinSymptomReservedNames(provider BuiltinSymptomMessages) []string {
	seen := make(map[string]struct{})
	names := make([]string, 0)

	add := func(raw string) {
		normalized := normalizeSymptomSpacing(raw)
		if normalized == "" {
			return
		}
		key := normalizeSymptomNameKey(normalized)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		names = append(names, normalized)
	}

	for _, symptom := range models.DefaultBuiltinSymptoms() {
		add(symptom.Name)
	}

	if provider == nil || isNilBuiltinSymptomMessages(provider) {
		return names
	}

	for _, language := range provider.SupportedLanguages() {
		messages := provider.Messages(language)
		for _, symptom := range models.DefaultBuiltinSymptoms() {
			if localized := strings.TrimSpace(messages[symptom.TranslationKey]); localized != "" {
				add(localized)
			}
		}
	}

	return names
}

func isNilBuiltinSymptomMessages(provider BuiltinSymptomMessages) bool {
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func builtinSymptomByName(name string) (models.BuiltinSymptom, bool) {
	target := normalizeSymptomNameKey(name)
	for _, symptom := range models.DefaultBuiltinSymptoms() {
		if normalizeSymptomNameKey(symptom.Name) == target {
			return symptom, true
		}
	}
	return models.BuiltinSymptom{}, false
}
