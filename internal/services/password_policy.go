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

// ValidatePasswordStrength enforces the two ends of the length rule in
// deliberately DIFFERENT units, because the reasons behind them differ: the
// minimum is a count of code points (what a person perceives as typing eight
// characters), while the maximum is a count of bytes (bcrypt's input limit,
// which knows nothing about code points). A passphrase in a non-Latin script
// therefore reaches the maximum at roughly half the character count of a Latin
// one, and emoji sooner still. Owner-facing copy must say so — the
// auth.password_requirements / auth.error.weak_password entries in every locale
// state the maximum in Latin characters and warn that other scripts count for
// more. Regression: TestPasswordLengthCopyStatesTheMaximumInTheUnitEnforced.
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
