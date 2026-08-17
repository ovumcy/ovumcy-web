package services

import (
	"errors"
	"unicode"
)

// ErrWeakPassword is the composition failure: too few characters, or missing
// one of the required character classes.
var ErrWeakPassword = errors.New("weak password")

// ErrPasswordTooLong is the length failure, kept SEPARATE from ErrWeakPassword
// on purpose. While both refusals shared one sentinel, the single message had
// to recite every requirement even when exactly one of them was broken, and the
// recital could not name the only fact that helps here — that the limit is
// counted in bytes. Whoever pasted a long non-Latin passphrase was told to add
// an uppercase letter it already had.
var ErrPasswordTooLong = errors.New("password too long")

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
// one, and emoji sooner still. Owner-facing copy must say so — every locale's
// auth.password_requirements and auth.error.password_too_long carry the
// qualifier that letters outside the Latin alphabet count as several.
//
// PRECEDENCE, for a password that breaks more than one rule at once:
//
//   - Too long BEATS the character classes. Shortening is required either way,
//     while adding the missing class to an over-long password changes nothing —
//     reporting the class first would send someone through an edit that cannot
//     succeed. Regression: TestValidatePasswordStrengthReportsTooLongBeforeMissingClasses.
//   - Too short and too long cannot both hold: the longest UTF-8 rune is four
//     bytes, so seven runes reach 28 bytes at most and can never exceed 72.
//     Their relative order here is therefore unobservable.
//
// Regression: TestValidatePasswordStrengthDistinguishesTooLongFromWeak.
func ValidatePasswordStrength(password string) error {
	if len([]rune(password)) < 8 {
		return ErrWeakPassword
	}
	if len(password) > maxPasswordBytes {
		return ErrPasswordTooLong
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
