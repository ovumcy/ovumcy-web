package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// authTokenDomain is one purpose a signed token can be minted for. Three
// things separate the purposes, and the parser checks all of them: the HMAC
// key (derived per purpose from SECRET_KEY, see security.DeriveTokenSigningKey),
// the `aud` claim, and the `typ` header. The key alone is what makes a
// cross-purpose token fail — the audience and type make the refusal explicit
// in the token itself, so a future purpose added on a shared key would still
// not accept the others' tokens.
type authTokenDomain struct {
	keyLabel string
	audience string
	typ      string
}

var (
	authSessionTokenDomain = authTokenDomain{
		keyLabel: security.AuthSessionTokenKeyLabel,
		audience: "ovumcy:auth-session",
		typ:      "ovumcy-auth-session+jwt",
	}
	passwordResetTokenDomain = authTokenDomain{
		keyLabel: security.PasswordResetTokenKeyLabel,
		audience: "ovumcy:password-reset",
		typ:      "ovumcy-password-reset+jwt",
	}
)

var errAuthTokenWrongType = errors.New("auth token: wrong type header")

// sign stamps the domain's audience onto registered (the claims' embedded
// RegisteredClaims) and signs claims under the domain's derived key.
func (domain authTokenDomain) sign(secretKey []byte, claims jwt.Claims, registered *jwt.RegisteredClaims) (string, error) {
	key, err := security.DeriveTokenSigningKey(secretKey, domain.keyLabel)
	if err != nil {
		return "", err
	}
	registered.Audience = jwt.ClaimStrings{domain.audience}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["typ"] = domain.typ
	return token.SignedString(key)
}

// parse verifies rawToken for this domain: HS256 only, the domain's `typ`
// header, a signature under the domain's derived key, and the domain's `aud`.
// A token whose header or method is wrong is refused before its signature is
// checked, so it is never reported as merely expired.
func (domain authTokenDomain) parse(secretKey []byte, rawToken string, claims jwt.Claims, now time.Time) (*jwt.Token, error) {
	key, err := security.DeriveTokenSigningKey(secretKey, domain.keyLabel)
	if err != nil {
		return nil, err
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(func() time.Time { return now }),
		jwt.WithAudience(domain.audience),
	)
	return parser.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		if typ, _ := token.Header["typ"].(string); typ != domain.typ {
			return nil, errAuthTokenWrongType
		}
		return key, nil
	})
}

func signAuthSessionClaims(secretKey []byte, claims *AuthSessionClaims) (string, error) {
	return authSessionTokenDomain.sign(secretKey, claims, &claims.RegisteredClaims)
}

func signPasswordResetClaims(secretKey []byte, claims *PasswordResetClaims) (string, error) {
	return passwordResetTokenDomain.sign(secretKey, claims, &claims.RegisteredClaims)
}
