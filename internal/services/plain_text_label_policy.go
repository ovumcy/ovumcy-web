package services

import "unicode"

func containsInvalidPlainTextLabelRune(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
		switch r {
		case '<', '>':
			return true
		}
	}
	return false
}
