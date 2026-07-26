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

// NormalizeAuthEmail canonicalizes an auth email input: lowercased, trimmed,
// and STRICTLY a bare RFC 5322 addr-spec. Anything the parser accepts but that
// is not byte-identical to the bare address — a display name
// ("john doe <a@b.com>"), a comment, a quoted-string local part the parser
// would decode — is rejected rather than silently rewritten: the stored
// identity must be exactly the string a later login types. (Returning the
// parsed .Address instead would silently rewrite input AND break idempotency:
// the decoded form of a quoted local part no longer re-parses.) Before this
// rule, the whole decorated input was stored verbatim, so "a@b.com" and
// "john doe <a@b.com>" could coexist as two accounts on one mailbox and the
// registration duplicate check missed them; rows written back then are
// repaired once at boot by AuthEmailRenormalizer.
func NormalizeAuthEmail(raw string) string {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return ""
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
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
