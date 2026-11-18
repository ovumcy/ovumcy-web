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
	// ErrAuthSessionLookupFailed is a storage fault while resolving a well-formed
	// token: it says nothing about the session, so the caller must not treat it
	// as a refusal that ends one.
	ErrAuthSessionLookupFailed = errors.New("auth session lookup failed")
	// ErrAuthSessionLifetimeOverPolicy is a token sealed with a longer lifetime
	// than the instance issues now. It is an expiry, so errors.Is matches both.
	ErrAuthSessionLifetimeOverPolicy = fmt.Errorf("%w: lifetime over policy", ErrAuthSessionTokenExpired)
)

// DefaultMaxAuthSessionLifetime is the longest session lifetime an AuthService
// accepts until ConfigureMaxSessionLifetime says otherwise: the default
// remember-me lifetime.
const DefaultMaxAuthSessionLifetime = 30 * 24 * time.Hour

// authSessionLifetimeWithinPolicy is a filter, not a revocation: it judges the
// token against the ceiling in force now, so a token refused under a lowered
// ceiling resolves again if the ceiling is raised back. Revoking takes an
// AuthSessionVersion bump. A token missing either date is refused: every token
// minted here carries both, and a missing expiry would mean a session that
// never ends.
func authSessionLifetimeWithinPolicy(claims *AuthSessionClaims, ceiling time.Duration) bool {
	if claims == nil || claims.IssuedAt == nil || claims.ExpiresAt == nil {
		return false
	}
	return claims.ExpiresAt.Sub(claims.IssuedAt.Time) <= ceiling
}

type AuthSessionClaims struct {
	UserID         uint   `json:"uid"`
	Role           string `json:"role"`
	SessionVersion int    `json:"sv"`
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
	// Refused, never normalized to 1, the same as both grant parsers: a token
	// with no epoch would otherwise match every account still at version 1.
	if claims.SessionVersion < 1 {
		return nil, ErrAuthSessionTokenInvalid
	}
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
