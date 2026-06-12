package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// registerPickupCookieTTL caps how long the sealed register-pickup cookie is
// honored after POST /api/v1/users. Short on purpose so a stale pickup
// cannot be replayed minutes after the fact, but long enough to absorb the
// natural 303 follow-up plus a brief stall.
const registerPickupCookieTTL = 5 * time.Minute

// registerPickupNonceBytes controls the entropy of the opaque per-pickup
// nonce. 16 bytes (128 bits) is well above the birthday-collision floor for
// the 5-minute window and the per-IP register rate limit, and is identical
// for real and decoy payloads so the sealed cookie length stays oracle-safe.
const registerPickupNonceBytes = 16

// registerPickupPayload carries the state needed to materialize an auth
// session and reveal a recovery code at GET /register/welcome. The payload is
// serialized to JSON with FIXED-WIDTH string fields so that the resulting
// ciphertext is byte-identical in length between a real new-user payload and
// a decoy payload for a duplicate-email collision. This is what closes the
// per-request Set-Cookie enumeration oracle on POST /api/v1/users.
//
// The Nonce field is opaque: for a real pickup it is the primary key of a
// server-side `register_pickup_tokens` row whose Consume() call resolves to
// the actual user_id and atomically marks the token used. For a decoy it is
// random bytes that no row exists for, so Consume() returns "not found" and
// the welcome handler falls through to the same /login redirect as a stale
// or already-consumed pickup. This is the server-side single-use guarantee.
type registerPickupPayload struct {
	Nonce string `json:"nonce"` // 32 hex chars: opaque single-use handle (real and decoy share this shape)
	RC    string `json:"rc"`    // 19 chars: OVUM-XXXX-XXXX-XXXX recovery code (real or decoy in matching shape)
	EXP   string `json:"exp"`   // 16 hex chars: int64 unix nanos of expiry
}

func newRegisterPickupPayload(now time.Time, recoveryCode string) (registerPickupPayload, error) {
	rc := strings.TrimSpace(recoveryCode)
	if !validPickupRecoveryCodeShape(rc) {
		return registerPickupPayload{}, errors.New("pickup payload requires OVUM-XXXX-XXXX-XXXX recovery code")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nonce, err := generatePickupNonce()
	if err != nil {
		return registerPickupPayload{}, err
	}
	expiresHex, err := encodePickupExpiryHex(now.UTC().Add(registerPickupCookieTTL))
	if err != nil {
		return registerPickupPayload{}, err
	}
	return registerPickupPayload{
		Nonce: nonce,
		RC:    rc,
		EXP:   expiresHex,
	}, nil
}

// newRegisterPickupDecoyPayload returns a payload structurally indistinguishable
// from newRegisterPickupPayload but whose decrypted contents will never resolve
// to a real user. Use for the duplicate-email branch so that POST register
// emits the same Set-Cookie shape regardless of email existence.
func newRegisterPickupDecoyPayload(now time.Time) (registerPickupPayload, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nonce, err := generatePickupNonce()
	if err != nil {
		return registerPickupPayload{}, err
	}
	rcBytes := make([]byte, 6)
	if _, err := rand.Read(rcBytes); err != nil {
		return registerPickupPayload{}, err
	}
	rcHex := strings.ToUpper(hex.EncodeToString(rcBytes))
	rc := "OVUM-" + rcHex[0:4] + "-" + rcHex[4:8] + "-" + rcHex[8:12]
	expiresHex, err := encodePickupExpiryHex(now.UTC().Add(registerPickupCookieTTL))
	if err != nil {
		return registerPickupPayload{}, err
	}
	return registerPickupPayload{
		Nonce: nonce,
		RC:    rc,
		EXP:   expiresHex,
	}, nil
}

// generatePickupNonce returns a 32-hex-char (16-byte) random handle suitable
// for the sealed-cookie payload and as the primary key of the corresponding
// register_pickup_tokens row.
func generatePickupNonce() (string, error) {
	buffer := make([]byte, registerPickupNonceBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// encodePickupExpiryHex renders the pickup expiry as a 16-char zero-padded hex
// string. Surfaces a clock anomaly (negative unix nanos) as an error instead
// of silently formatting as "-<hex>" and breaking the fixed-width invariant
// that the response-parity test depends on.
func encodePickupExpiryHex(expires time.Time) (string, error) {
	nanos := expires.UnixNano()
	if nanos < 0 {
		return "", errors.New("pickup expiry is before unix epoch")
	}
	return fmt.Sprintf("%016x", nanos), nil
}

// decodePickupExpiry parses the 16-char zero-padded hex back into a UTC time.
// strconv.ParseInt with bitSize=64 rejects values that do not fit in int64
// (an attacker-supplied "ffff...ff" returns an error), so no narrowing
// conversion is needed.
func decodePickupExpiry(encoded string) (time.Time, error) {
	if len(encoded) != 16 {
		return time.Time{}, errors.New("invalid pickup exp")
	}
	nanos, err := strconv.ParseInt(encoded, 16, 64)
	if err != nil {
		return time.Time{}, err
	}
	if nanos < 0 {
		return time.Time{}, errors.New("invalid pickup exp")
	}
	return time.Unix(0, nanos).UTC(), nil
}

func validPickupRecoveryCodeShape(code string) bool {
	if len(code) != 19 {
		return false
	}
	if !strings.HasPrefix(code, "OVUM-") {
		return false
	}
	if code[9] != '-' || code[14] != '-' {
		return false
	}
	return true
}

func (payload registerPickupPayload) expiresAt() (time.Time, error) {
	return decodePickupExpiry(payload.EXP)
}

func (payload registerPickupPayload) validAt(now time.Time) bool {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expiry, err := payload.expiresAt()
	if err != nil || !expiry.After(now.UTC()) {
		return false
	}
	if !validPickupRecoveryCodeShape(strings.TrimSpace(payload.RC)) {
		return false
	}
	return validPickupNonceShape(payload.Nonce)
}

// validPickupNonceShape returns true when the nonce is the expected
// 32-hex-char handle produced by generatePickupNonce. The shape check is
// purely structural; cryptographic validation happens at Consume time.
func validPickupNonceShape(nonce string) bool {
	if len(nonce) != registerPickupNonceBytes*2 {
		return false
	}
	if _, err := hex.DecodeString(nonce); err != nil {
		return false
	}
	return true
}

var registerPickupCookieSpec = sealedCookieSpec{name: registerPickupCookieName, path: "/"}

func (handler *Handler) setRegisterPickupCookie(c *fiber.Ctx, payload registerPickupPayload) error {
	if !payload.validAt(time.Now()) {
		return errors.New("pickup cookie payload is invalid")
	}

	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return handler.writeSealedCookie(c, registerPickupCookieSpec, serialized, time.Now().Add(registerPickupCookieTTL))
}

func (handler *Handler) popRegisterPickupCookie(c *fiber.Ctx) (registerPickupPayload, bool) {
	raw := strings.TrimSpace(c.Cookies(registerPickupCookieName))
	if raw == "" {
		return registerPickupPayload{}, false
	}
	handler.clearRegisterPickupCookie(c)

	codec, err := handler.cookieCodec()
	if err != nil {
		return registerPickupPayload{}, false
	}
	decoded, err := codec.open(registerPickupCookieName, raw)
	if err != nil {
		return registerPickupPayload{}, false
	}

	payload := registerPickupPayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return registerPickupPayload{}, false
	}
	if !payload.validAt(time.Now()) {
		return registerPickupPayload{}, false
	}
	return payload, true
}

func (handler *Handler) clearRegisterPickupCookie(c *fiber.Ctx) {
	handler.clearSealedCookie(c, registerPickupCookieSpec)
}
