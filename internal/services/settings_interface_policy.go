package services

import "strings"

type InterfaceSettingsUpdate struct {
	Language string
	Theme    string
}

func NormalizeInterfaceTheme(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "light":
		return "light"
	case "dark":
		return "dark"
	default:
		return ""
	}
}

func (service *SettingsService) ResolveInterfaceUpdateStatus() string {
	return "interface_updated"
}
