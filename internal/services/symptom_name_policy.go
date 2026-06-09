package services

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxSymptomNameLength = 40
	maxSymptomIconLength = 16
	defaultSymptomIcon   = "✨"
	defaultSymptomColor  = "#E8799F"
)

var (
	hexSymptomColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

func normalizeSymptomNameInput(raw string) (string, error) {
	name := normalizeSymptomSpacing(raw)
	if name == "" {
		return "", ErrSymptomNameRequired
	}
	if utf8.RuneCountInString(name) > maxSymptomNameLength {
		return "", ErrSymptomNameTooLong
	}
	if containsInvalidPlainTextLabelRune(name) {
		return "", ErrSymptomNameInvalidCharacters
	}
	return name, nil
}

func normalizeSymptomNameKey(raw string) string {
	return strings.ToLower(normalizeSymptomSpacing(raw))
}

func normalizeSymptomSpacing(raw string) string {
	fields := strings.Fields(strings.ToValidUTF8(raw, ""))
	return strings.TrimSpace(strings.Join(fields, " "))
}

// normalizeSymptomIconInput trims and validates a custom symptom icon. Empty
// input falls back to the default icon. Like the name and display-name label
// policy, markup-like ("<"/">") and control characters are rejected (not
// stripped), and the icon is length-bounded. Errors reuse the symptom-name
// error values so this rarely-hit (API-only) edge needs no new i18n keys.
func normalizeSymptomIconInput(raw string) (string, error) {
	icon := strings.TrimSpace(strings.ToValidUTF8(raw, ""))
	if icon == "" {
		return defaultSymptomIcon, nil
	}
	if utf8.RuneCountInString(icon) > maxSymptomIconLength {
		return "", ErrSymptomNameTooLong
	}
	if containsInvalidPlainTextLabelRune(icon) {
		return "", ErrSymptomNameInvalidCharacters
	}
	return icon, nil
}

func normalizeSymptomColorInput(raw string) (string, error) {
	color := strings.TrimSpace(strings.ToUpper(strings.ToValidUTF8(raw, "")))
	if color == "" {
		return "", ErrInvalidSymptomColor
	}
	if !hexSymptomColorPattern.MatchString(color) {
		return "", ErrInvalidSymptomColor
	}
	return color, nil
}

func resolveSymptomColorInput(raw string, fallback string) (string, error) {
	color := strings.TrimSpace(strings.ToUpper(strings.ToValidUTF8(raw, "")))
	if color == "" {
		color = strings.TrimSpace(strings.ToUpper(strings.ToValidUTF8(fallback, "")))
	}
	if color == "" {
		color = defaultSymptomColor
	}
	return normalizeSymptomColorInput(color)
}
