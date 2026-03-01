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
	"github.com/terraincognita07/ovumcy/internal/security"
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

func BuildPasswordResetToken(secretKey []byte, userID uint, passwordHash string, ttl time.Duration, now time.Time) (string, error) {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if now.IsZero() {
		now = time.Now()
	}

	passwordState := PasswordStateFingerprint(passwordHash)
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
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.Before(now) {
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

func PasswordStateFingerprint(passwordHash string) string {
	normalizedHash := strings.TrimSpace(passwordHash)
	if normalizedHash == "" {
		return ""
	}

	sum := sha256.Sum256([]byte("ovumcy.reset.password-state.v1:" + normalizedHash))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func IsPasswordStateFingerprintMatch(expected string, passwordHash string) bool {
	actual := PasswordStateFingerprint(passwordHash)
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
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
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
	if len(normalized) != 12 {
		return strings.ToUpper(strings.TrimSpace(raw))
	}
	return fmt.Sprintf("%s-%s-%s-%s", recoveryCodePrefix, normalized[:4], normalized[4:8], normalized[8:12])
}
