package services

import (
	"errors"
	"unicode"
)

var ErrWeakPassword = errors.New("weak password")

// maxPasswordBytes mirrors bcrypt's hard input limit: GenerateFromPassword
// rejects anything longer than 72 bytes, which without this guard surfaced
// as an opaque internal error instead of a stable validation error.
const maxPasswordBytes = 72

func ValidatePasswordStrength(password string) error {
	if len([]rune(password)) < 8 {
		return ErrWeakPassword
	}
	if len(password) > maxPasswordBytes {
		return ErrWeakPassword
	}

	hasUpper := false
	hasLower := false
	hasDigit := false
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasDigit = true
		}
	}

	if hasUpper && hasLower && hasDigit {
		return nil
	}
	return ErrWeakPassword
}
