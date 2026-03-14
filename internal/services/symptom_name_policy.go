package services

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxSymptomNameLength = 40
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

func normalizeSymptomIconInput(raw string) string {
	icon := strings.TrimSpace(strings.ToValidUTF8(raw, ""))
	if icon == "" {
		return defaultSymptomIcon
	}
	return icon
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
