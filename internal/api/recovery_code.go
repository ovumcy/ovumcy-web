package api

import (
	"github.com/terraincognita07/ovumcy/internal/services"
)

func generateRecoveryCodeHash() (string, string, error) {
	return services.GenerateRecoveryCodeHash()
}
