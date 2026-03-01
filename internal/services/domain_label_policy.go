package services

import (
	"strings"

	"github.com/terraincognita07/ovumcy/internal/models"
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

func RoleTranslationKey(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case models.RoleOwner:
		return "role.owner"
	case models.RolePartner:
		return "role.partner"
	default:
		return role
	}
}

func PhaseIcon(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "menstrual":
		return "🌙"
	case "follicular":
		return "🌸"
	case "ovulation":
		return "☀️"
	case "fertile":
		return "🌿"
	case "luteal":
		return "🍂"
	default:
		return "✨"
	}
}

func SymptomGroup(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cramps", "headache", "breast tenderness", "back pain":
		return "pain"
	case "mood swings", "fatigue", "irritability", "insomnia":
		return "mood"
	case "bloating", "nausea", "diarrhea", "constipation", "swelling", "food cravings":
		return "digestion"
	case "acne", "spotting":
		return "skin"
	default:
		return "other"
	}
}
