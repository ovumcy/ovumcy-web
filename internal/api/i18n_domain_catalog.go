package api

import (
	"github.com/terraincognita07/ovumcy/internal/services"
)

func phaseTranslationKey(phase string) string {
	return services.PhaseTranslationKey(phase)
}

func flowTranslationKey(flow string) string {
	return services.FlowTranslationKey(flow)
}

func roleTranslationKey(role string) string {
	return services.RoleTranslationKey(role)
}
