package services

import (
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func PhaseTranslationKey(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "menstrual":
		return "phases.menstrual"
	case "follicular":
		return "phases.follicular"
	case "ovulation":
		return "phases.ovulation"
	case "fertile":
		return "phases.fertile"
	case "luteal":
		return "phases.luteal"
	default:
		return "phases.unknown"
	}
}

func FlowTranslationKey(flow string) string {
	switch strings.ToLower(strings.TrimSpace(flow)) {
	case models.FlowSpotting:
		return "dashboard.flow.spotting"
	case models.FlowLight:
		return "dashboard.flow.light"
	case models.FlowMedium:
		return "dashboard.flow.medium"
	case models.FlowHeavy:
		return "dashboard.flow.heavy"
	default:
		return "dashboard.flow.none"
	}
}

func SexActivityTranslationKey(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case models.SexActivityProtected:
		return "dashboard.sex.protected"
	case models.SexActivityUnprotected:
		return "dashboard.sex.unprotected"
	default:
		return "dashboard.sex.none"
	}
}

func CervicalMucusTranslationKey(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case models.CervicalMucusDry:
		return "dashboard.cervical_mucus.dry"
	case models.CervicalMucusMoist:
		return "dashboard.cervical_mucus.moist"
	case models.CervicalMucusCreamy:
		return "dashboard.cervical_mucus.creamy"
	case models.CervicalMucusEggWhite:
		return "dashboard.cervical_mucus.eggwhite"
	default:
		return "dashboard.cervical_mucus.none"
	}
}

func RoleTranslationKey(role string) string {
	switch NormalizeUserRole(role) {
	case models.RoleOwner:
		return "role.owner"
	default:
		return role
	}
}

func PhaseIcon(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "menstrual":
		return "\U0001FA78"
	case "follicular":
		return "\U0001F338"
	case "ovulation":
		return "\u2600\uFE0F"
	case "fertile":
		return "\U0001F33F"
	case "luteal":
		return "\U0001F342"
	default:
		return "\u2728"
	}
}
