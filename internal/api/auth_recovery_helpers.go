package api

import (
	"github.com/terraincognita07/ovumcy/internal/services"
)

func normalizeLoginEmail(raw string) string {
	return services.NormalizeAuthEmail(raw)
}
