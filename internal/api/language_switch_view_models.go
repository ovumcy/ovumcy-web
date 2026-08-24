package api

import (
	"fmt"
	"strings"
)

type languageSwitchOption struct {
	Code   string
	Label  string
	Active bool
}

func buildLanguageSwitchOptions(messages map[string]string, currentLanguage string, supported []string) []languageSwitchOption {
	options := make([]languageSwitchOption, 0, len(supported))
	for _, code := range supported {
		normalizedCode := strings.TrimSpace(code)
		if normalizedCode == "" {
			continue
		}
		options = append(options, languageSwitchOption{
			Code:   normalizedCode,
			Label:  localizedLanguageSwitchLabel(messages, normalizedCode),
			Active: normalizedCode == currentLanguage,
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
