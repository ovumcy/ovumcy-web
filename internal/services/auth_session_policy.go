package services

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
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

// BuildAuthSessionTokenWithVersionAndSessionID mints an auth session token and
// returns the session id it embedded. It is the only builder: a caller that has
// no use for the session id discards it, rather than reaching for a wrapper that
// hides which session was created.
//
// The role travels into the claims unchecked on purpose. Whether a role may hold
// a web session is decided by ValidateSupportedWebUser at the two places that
// matter — where the api layer issues a cookie, and where ResolveAuthSession
// admits one — so this stays a pure minting primitive.
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
		SessionVersion: NormalizeAuthSessionVersion(sessionVersion),
		SessionID:      sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(uint64(userID), 10),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	// Signed under the auth-session key domain (its own HKDF-derived key, `aud`
	// and `typ`), never under SECRET_KEY itself: see authTokenDomain.
	rawToken, signErr := signAuthSessionClaims(secretKey, &claims)
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
	token, err := authSessionTokenDomain.parse(secretKey, rawToken, claims, now)
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
	claims.SessionVersion = NormalizeAuthSessionVersion(claims.SessionVersion)
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

// AuthSessionVersionsMatch reports whether a version a grant was minted at is
// still the account's current one. Both sides are normalized, so a legacy zero
// on the row compares as 1 — the same reading ResolveAuthSession applies.
func AuthSessionVersionsMatch(granted int, current int) bool {
	return NormalizeAuthSessionVersion(granted) == NormalizeAuthSessionVersion(current)
}

func NormalizeAuthSessionVersion(version int) int {
	if version <= 0 {
		return 1
	}
	return version
}
