package api

import (
	"github.com/terraincognita07/ovumcy/internal/services"
)

func normalizeRecoveryCode(raw string) string {
	return services.NormalizeRecoveryCode(raw)
}

func generateRecoveryCodeHash() (string, string, error) {
	return services.GenerateRecoveryCodeHash()
}
