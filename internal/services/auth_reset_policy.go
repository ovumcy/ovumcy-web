package services

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"golang.org/x/crypto/bcrypt"
)

const (
	recoveryCodePrefix        = "OVUM"
	passwordResetTokenPurpose = "password_reset"
)

var (
	ErrPasswordResetTokenMissing              = errors.New("missing reset token")
	ErrPasswordResetTokenInvalid              = errors.New("invalid reset token")
	ErrPasswordResetTokenInvalidPurpose       = errors.New("invalid reset token purpose")
	ErrPasswordResetTokenExpired              = errors.New("expired reset token")
	ErrPasswordResetTokenInvalidUserID        = errors.New("invalid reset token user id")
	ErrPasswordResetTokenInvalidPasswordState = errors.New("invalid reset token password state")
)

type PasswordResetClaims struct {
	UserID        uint   `json:"uid"`
	Purpose       string `json:"purpose"`
	PasswordState string `json:"password_state"`
	jwt.RegisteredClaims
}

// BuildPasswordResetToken mints the reset token. storedHash is the account's
// bcrypt hash as read from the row — never a raw password; the caller has one
// only in the shape it already persists.
func BuildPasswordResetToken(secretKey []byte, userID uint, storedHash string, ttl time.Duration, now time.Time) (string, error) {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if now.IsZero() {
		now = time.Now()
	}

	passwordState := PasswordStateFingerprint(storedHash)
	if passwordState == "" {
		return "", ErrPasswordResetTokenInvalidPasswordState
	}

	claims := PasswordResetClaims{
		UserID:        userID,
		Purpose:       passwordResetTokenPurpose,
		PasswordState: passwordState,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(uint64(userID), 10),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secretKey)
}

func ParsePasswordResetToken(secretKey []byte, rawToken string, now time.Time) (*PasswordResetClaims, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, ErrPasswordResetTokenMissing
	}
	if now.IsZero() {
		now = time.Now()
	}

	claims := &PasswordResetClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	token, err := parser.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secretKey, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrPasswordResetTokenExpired
		}
		return nil, ErrPasswordResetTokenInvalid
	}
	if !token.Valid {
		return nil, ErrPasswordResetTokenInvalid
	}
	if claims.Purpose != passwordResetTokenPurpose {
		return nil, ErrPasswordResetTokenInvalidPurpose
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Before(now) {
		return nil, ErrPasswordResetTokenExpired
	}
	if claims.UserID == 0 {
		return nil, ErrPasswordResetTokenInvalidUserID
	}
	if strings.TrimSpace(claims.PasswordState) == "" {
		return nil, ErrPasswordResetTokenInvalidPasswordState
	}
	return claims, nil
}

// PasswordStateFingerprint reduces an account's ALREADY-HASHED credential to a
// short change-detection value carried inside a reset token.
//
// storedHash is the bcrypt hash read from `users.password_hash`. A raw password
// never reaches this function and must never be passed to it: the only callers
// read the column, and the raw secret a request carries is spent exclusively on
// `bcrypt.CompareHashAndPassword`. The SHA-256 below is therefore **not**
// password hashing and must not be read as such — it fingerprints a value that
// is already a slow, salted hash, so that a token dies the moment the
// credential behind it changes. `ResolveUserByResetToken` recomputes this over
// the current row and refuses a token whose fingerprint no longer matches,
// which is what makes a reset token effectively one-time.
//
// A password KDF would be the wrong primitive here and buy nothing: the input
// is not low-entropy, it is not a secret an attacker can test candidates
// against, and the comparison must be cheap enough to run on every redeem.
func PasswordStateFingerprint(storedHash string) string {
	normalizedHash := strings.TrimSpace(storedHash)
	if normalizedHash == "" {
		return ""
	}

	sum := sha256.Sum256([]byte("ovumcy.reset.password-state.v1:" + normalizedHash))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// IsPasswordStateFingerprintMatch compares a token's carried fingerprint with
// one recomputed over storedHash — again the bcrypt hash from the row, never a
// raw password. See PasswordStateFingerprint.
func IsPasswordStateFingerprintMatch(expected string, storedHash string) bool {
	actual := PasswordStateFingerprint(storedHash)
	if strings.TrimSpace(expected) == "" || strings.TrimSpace(actual) == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func GenerateRecoveryCodeHash() (string, string, error) {
	code, err := GenerateRecoveryCode()
	if err != nil {
		return "", "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(code), passwordHashCost)
	if err != nil {
		return "", "", err
	}
	return code, string(hash), nil
}

func GenerateRecoveryCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	value, err := security.RandomString(12, alphabet)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s-%s-%s-%s", recoveryCodePrefix, value[:4], value[4:8], value[8:12]), nil
}

func NormalizeRecoveryCode(raw string) string {
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.TrimPrefix(normalized, recoveryCodePrefix)
	// Only reformat when the body is exactly 12 ASCII alphanumerics. A byte-length
	// check plus byte slicing is unsafe for multi-byte/invalid-UTF-8 input, where
	// ToUpper can change the byte length and a slice can split a rune, yielding
	// unstable, non-idempotent output. Recovery code bodies are always [A-Z0-9].
	if !isCanonicalRecoveryCodeBody(normalized) {
		return strings.ToUpper(strings.TrimSpace(raw))
	}
	return fmt.Sprintf("%s-%s-%s-%s", recoveryCodePrefix, normalized[:4], normalized[4:8], normalized[8:12])
}

// isCanonicalRecoveryCodeBody reports whether value is exactly 12 ASCII
// alphanumeric characters (the canonical recovery-code body). Because every
// accepted character is single-byte ASCII, byte indexing the result is safe.
func isCanonicalRecoveryCodeBody(value string) bool {
	if len(value) != 12 {
		return false
	}
	for i := range len(value) {
		c := value[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
