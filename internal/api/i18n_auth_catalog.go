package api

import "github.com/terraincognita07/ovumcy/internal/services"

func authErrorTranslationKey(message string) string {
	return services.AuthErrorTranslationKey(message)
}
