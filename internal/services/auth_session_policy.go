package services

import (
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
)

type AuthSessionClaims struct {
	UserID uint   `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func BuildAuthSessionToken(secretKey []byte, userID uint, role string, ttl time.Duration, now time.Time) (string, error) {
	if userID == 0 {
		return "", ErrAuthSessionTokenInvalidUserID
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	if now.IsZero() {
		now = time.Now()
	}

	claims := AuthSessionClaims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(uint64(userID), 10),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secretKey)
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
	return claims, nil
}

