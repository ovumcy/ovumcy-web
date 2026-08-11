package services

import "strings"

type InterfaceSettingsUpdate struct {
	Language string
	Theme    string
}

// NormalizeInterfaceTheme accepts the three theme preferences the interface
// form can submit. The theme itself stays client-side (localStorage), so the
// server only validates the value; "system" is a standing instruction to follow
// the browser's prefers-color-scheme, resolved in the client at apply time.
func NormalizeInterfaceTheme(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "light":
		return "light"
	case "dark":
		return "dark"
	case "system":
		return "system"
	default:
		return ""
	}
}

// SettingsInterfaceUpdatedStatus is the flash status emitted after a
// successful interface-settings save (always the same outcome).
const SettingsInterfaceUpdatedStatus = "interface_updated"
