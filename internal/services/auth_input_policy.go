package services

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
)

var (
	ErrAuthCredentialsInvalid  = errors.New("auth credentials invalid")
	ErrAuthRecoveryCodeInvalid = errors.New("auth recovery code invalid")
	ErrAuthResetInputInvalid   = errors.New("auth reset input invalid")
)

var recoveryCodeFormatRegex = regexp.MustCompile(`^OVUM-[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}$`)

func NormalizeAuthEmail(raw string) string {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return ""
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return ""
	}
	return email
}

func NormalizeCredentialsInput(emailRaw string, passwordRaw string) (string, string, error) {
	email := NormalizeAuthEmail(emailRaw)
	password := strings.TrimSpace(passwordRaw)
	if email == "" || password == "" {
		return "", "", ErrAuthCredentialsInvalid
	}
	return email, password, nil
}

func ValidateRecoveryCodeFormat(code string) error {
	if !recoveryCodeFormatRegex.MatchString(strings.TrimSpace(code)) {
		return ErrAuthRecoveryCodeInvalid
	}
	return nil
}

func NormalizeForgotPasswordCode(raw string) (string, error) {
	code := strings.TrimSpace(raw)
	if code == "" {
		return "", ErrAuthRecoveryCodeInvalid
	}
	return code, nil
}

func NormalizeResetPasswordInput(passwordRaw string, confirmRaw string) (string, string, error) {
	password := strings.TrimSpace(passwordRaw)
	confirmPassword := strings.TrimSpace(confirmRaw)
	if password == "" || confirmPassword == "" {
		return "", "", ErrAuthResetInputInvalid
	}
	return password, confirmPassword, nil
}
