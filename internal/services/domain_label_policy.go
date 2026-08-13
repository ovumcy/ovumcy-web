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

func PregnancyTestTranslationKey(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case models.PregnancyTestNegative:
		return "dashboard.pregnancy_test.negative"
	case models.PregnancyTestPositive:
		return "dashboard.pregnancy_test.positive"
	default:
		return "dashboard.pregnancy_test.none"
	}
}

// MoodTranslationKey names a step of the mood scale. The faces alone leave the
// scale to be guessed — two people picking the third one were recording
// different things — so every step carries a name, and the name is a catalogue
// key rather than a glyph or a fraction. A value outside the scale has no name;
// callers render their own no-data label for it.
func MoodTranslationKey(value int) string {
	switch value {
	case 1:
		return "dashboard.mood.very_low"
	case 2:
		return "dashboard.mood.low"
	case 3:
		return "dashboard.mood.neutral"
	case 4:
		return "dashboard.mood.good"
	case 5:
		return "dashboard.mood.very_good"
	default:
		return ""
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

// PhaseIcon names the icon a phase is drawn with. The value is a key into the
// first-party icon set the templates render, not a glyph: emoji rendered as
// text were read out as page content ("cherry blossom", "maple leaf") and drew
// differently on every platform.
func PhaseIcon(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "menstrual":
		return "drop"
	case "follicular":
		return "sprout"
	case "ovulation":
		return "sun"
	case "luteal":
		return "leaf"
	default:
		return "sparkle"
	}
}
