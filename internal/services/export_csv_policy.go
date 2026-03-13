package services

import "strings"

func sanitizeCSVTextCell(value string) string {
	if value == "" {
		return ""
	}

	if hasDangerousCSVPrefix(value) {
		return "'" + value
	}
	return value
}

func hasDangerousCSVPrefix(value string) bool {
	trimmed := strings.TrimLeft(value, " ")
	if trimmed == "" {
		return false
	}

	switch trimmed[0] {
	case '=', '+', '-', '@', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}
