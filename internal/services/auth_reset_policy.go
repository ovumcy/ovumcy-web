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
	recoveryCodePrefix = "OVUM"

	// PasswordResetTokenPurposeRecovery marks a token minted by the email +
	// recovery-code flow (ForgotPassword/StartRecovery). It is the only
	// purpose ForgotPassword itself gates on the instance-wide local-sign-in
	// toggle, so its redeem must be gated identically.
	PasswordResetTokenPurposeRecovery = "password_reset_recovery"
	// PasswordResetTokenPurposeForcedOIDC marks a token minted after an OIDC
	// sign-in (or OIDC link-confirm's own OIDC identity attach) resolves to an
	// account carrying MustChangePassword. An oidc_only instance legitimately
	// mints and redeems these with local sign-in switched off.
	PasswordResetTokenPurposeForcedOIDC = "password_reset_forced_oidc"
	// PasswordResetTokenPurposeForcedLocal marks a token minted after a LOCAL
	// password authenticates successfully against an account carrying
	// MustChangePassword — the plain login route, and OIDC link-confirm's own
	// password challenge (LoginService.Authenticate gates both). Unlike the
	// OIDC purpose, this one must NOT survive the instance toggle being off:
	// the factor that produced it is exactly the one the operator disabled.
	PasswordResetTokenPurposeForcedLocal = "password_reset_forced_local"
)

// passwordResetTokenAllowedPurposes is the redeem-time allow-list: an
// unrecognised or legacy Purpose (including the single pre-migration value
// every reset token carried before this split) refuses here rather than
// being reinterpreted as one of the three current values. BuildPasswordResetToken
// mints only members of this set.
var passwordResetTokenAllowedPurposes = map[string]bool{
	PasswordResetTokenPurposeRecovery:    true,
	PasswordResetTokenPurposeForcedOIDC:  true,
	PasswordResetTokenPurposeForcedLocal: true,
}

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
// only in the shape it already persists. purpose must be one of the three
// PasswordResetTokenPurpose* constants — it is what ParsePasswordResetToken's
// redeem-time allow-list decides on, not any out-of-band signal such as a
// cookie-carried bool.
func BuildPasswordResetToken(secretKey []byte, userID uint, storedHash string, purpose string, ttl time.Duration, now time.Time) (string, error) {
	if !passwordResetTokenAllowedPurposes[purpose] {
		return "", ErrPasswordResetTokenInvalidPurpose
	}
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
		Purpose:       purpose,
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
	if !passwordResetTokenAllowedPurposes[claims.Purpose] {
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

// PasswordResetTokenRefusedByLocalAuthGate reports whether the
// instance-wide local-sign-in toggle (localPublicAuthEnabled) is the correct
// reason to refuse rawToken right now. It answers true only when the token
// parses successfully — valid signature, not expired — and its SIGNED
// purpose is recovery or forced-from-LOCAL: the two purposes produced by (or
// gated identically to) a local password, which is exactly the factor the
// operator disabled. A forced-from-OIDC token answers false: an oidc_only
// instance legitimately mints and must keep redeeming those with local
// sign-in switched off.
//
// A token that fails to parse for ANY reason — expired, malformed, wrong
// signature, or an unrecognised/legacy purpose — also answers false. That is
// deliberate: such a token is invalid on its own terms, and the caller must
// let the ordinary invalid-token path (ResolveUserByResetToken/CompleteReset,
// or IsResetPasswordTokenValid on the page) say so. Collapsing every parse
// failure into "the instance toggle refused this" mislabels routine expiry —
// the default outcome of every forced-from-OIDC reset a user takes longer
// than the 30-minute TTL to complete — as an operator-configuration refusal,
// and floods the security log with a local-recovery-disabled entry that
// never happened. The actual security property does not depend on which of
// the two refusal paths answers: a token that fails to parse here still
// fails to parse in CompleteReset, so redemption is refused either way — only
// the message and the logged reason change.
func PasswordResetTokenRefusedByLocalAuthGate(secretKey []byte, rawToken string, now time.Time) bool {
	claims, err := ParsePasswordResetToken(secretKey, rawToken, now)
	if err != nil {
		return false
	}
	return claims.Purpose != PasswordResetTokenPurposeForcedOIDC
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
