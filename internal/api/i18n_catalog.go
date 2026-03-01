package api

import "github.com/terraincognita07/ovumcy/internal/services"

func settingsStatusTranslationKey(status string) string {
	return services.SettingsStatusTranslationKey(status)
}
