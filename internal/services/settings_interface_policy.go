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

// SettingsInterfaceUpdatedStatus is the flash status emitted after a
// successful interface-settings save (always the same outcome).
const SettingsInterfaceUpdatedStatus = "interface_updated"
