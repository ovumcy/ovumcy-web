package services

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrAuthSessionTokenMissing       = errors.New("auth session token missing")
	ErrAuthSessionTokenInvalid       = errors.New("auth session token invalid")
	ErrAuthSessionTokenExpired       = errors.New("auth session token expired")
	ErrAuthSessionTokenInvalidUserID = errors.New("auth session token invalid user id")
	ErrAuthSessionTokenRevoked       = errors.New("auth session token revoked")
)

type AuthSessionClaims struct {
	UserID         uint   `json:"uid"`
	Role           string `json:"role"`
	SessionVersion int    `json:"sv,omitempty"`
	SessionID      string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

func BuildAuthSessionToken(secretKey []byte, userID uint, role string, ttl time.Duration, now time.Time) (string, error) {
	token, _, err := BuildAuthSessionTokenWithVersionAndSessionID(secretKey, userID, role, 1, ttl, now)
	return token, err
}

func BuildAuthSessionTokenWithVersion(secretKey []byte, userID uint, role string, sessionVersion int, ttl time.Duration, now time.Time) (string, error) {
	token, _, err := BuildAuthSessionTokenWithVersionAndSessionID(secretKey, userID, role, sessionVersion, ttl, now)
	return token, err
}

func BuildAuthSessionTokenWithVersionAndSessionID(secretKey []byte, userID uint, role string, sessionVersion int, ttl time.Duration, now time.Time) (string, string, error) {
	if userID == 0 {
		return "", "", ErrAuthSessionTokenInvalidUserID
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	if now.IsZero() {
		now = time.Now()
	}
	sessionID, err := GenerateAuthSessionID()
	if err != nil {
		return "", "", err
	}

	claims := AuthSessionClaims{
		UserID:         userID,
		Role:           role,
		SessionVersion: normalizeAuthSessionVersion(sessionVersion),
		SessionID:      sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(uint64(userID), 10),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	rawToken, signErr := token.SignedString(secretKey)
	if signErr != nil {
		return "", "", signErr
	}
	return rawToken, sessionID, nil
}

func ParseAuthSessionToken(secretKey []byte, rawToken string, now time.Time) (*AuthSessionClaims, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, ErrAuthSessionTokenMissing
	}
	if now.IsZero() {
		now = time.Now()
	}

	claims := &AuthSessionClaims{}
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
			return nil, ErrAuthSessionTokenExpired
		}
		return nil, ErrAuthSessionTokenInvalid
	}
	if !token.Valid {
		return nil, ErrAuthSessionTokenInvalid
	}
	if claims.UserID == 0 {
		return nil, ErrAuthSessionTokenInvalidUserID
	}
	claims.SessionVersion = normalizeAuthSessionVersion(claims.SessionVersion)
	claims.SessionID = strings.TrimSpace(claims.SessionID)
	if claims.SessionID == "" {
		return nil, ErrAuthSessionTokenInvalid
	}
	return claims, nil
}

func GenerateAuthSessionID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func normalizeAuthSessionVersion(version int) int {
	if version <= 0 {
		return 1
	}
	return version
}
