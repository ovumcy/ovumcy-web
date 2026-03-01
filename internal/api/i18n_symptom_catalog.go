package api

import "github.com/terraincognita07/ovumcy/internal/services"

func localizedSymptomName(messages map[string]string, name string) string {
	key := services.BuiltinSymptomTranslationKey(name)
	if key == "" {
		return name
	}
	return translateMessage(messages, key)
}
