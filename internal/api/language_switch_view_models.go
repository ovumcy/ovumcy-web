package api

import (
	"fmt"
	"strings"
)

type languageSwitchOption struct {
	Code  string
	Label string
	// AccessibleName spells the language in its own script and keeps the visible
	// Label inside it, so a screen reader does not announce a bare "de".
	AccessibleName string
	Active         bool
}

var languageEndonyms = map[string]string{
	"de": "Deutsch",
	"en": "English",
	"es": "Español",
	"fr": "Français",
	"it": "Italiano",
	"ru": "Русский",
}

func buildLanguageSwitchOptions(messages map[string]string, currentLanguage string, supported []string) []languageSwitchOption {
	options := make([]languageSwitchOption, 0, len(supported))
	for _, code := range supported {
		normalizedCode := strings.TrimSpace(code)
		if normalizedCode == "" {
			continue
		}
		label := localizedLanguageSwitchLabel(messages, normalizedCode)
		accessibleName := label
		if endonym, known := languageEndonyms[normalizedCode]; known {
			accessibleName = endonym + " (" + label + ")"
		}
		options = append(options, languageSwitchOption{
			Code:           normalizedCode,
			Label:          label,
			AccessibleName: accessibleName,
			Active:         normalizedCode == currentLanguage,
		})
	}
	return options
}

func localizedLanguageSwitchLabel(messages map[string]string, code string) string {
	key := fmt.Sprintf("lang.%s", code)
	localized, translated := lookupMessage(messages, key)
	if !translated {
		return strings.ToUpper(code)
	}
	return localized
}
