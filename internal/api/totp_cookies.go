package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

const totpPendingCookieTTL = 5 * time.Minute

type totpPendingCookiePayload struct {
	UserID     uint      `json:"user_id"`
	RememberMe bool      `json:"remember_me,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type totpSetupCookiePayload struct {
	UserID    uint      `json:"user_id"`
	RawSecret string    `json:"raw_secret"`
	ExpiresAt time.Time `json:"expires_at"`
}

// setTOTPPendingCookie writes a short-lived sealed cookie that carries the user's
// ID and rememberMe flag across the 2FA challenge step.
var (
	totpPendingCookieSpec = sealedCookieSpec{name: totpPendingCookieName, path: "/"}
	totpSetupCookieSpec   = sealedCookieSpec{name: totpSetupCookieName, path: "/"}
)

func (handler *Handler) setTOTPPendingCookie(c fiber.Ctx, userID uint, rememberMe bool) error {
	payload := totpPendingCookiePayload{
		UserID:     userID,
		RememberMe: rememberMe,
		ExpiresAt:  time.Now().Add(totpPendingCookieTTL),
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Session-scoped (zero expires) — the payload carries its own ExpiresAt.
	return handler.writeSealedCookie(c, totpPendingCookieSpec, serialized, time.Time{})
}

// parseTOTPPendingCookie decodes and validates the TOTP pending cookie.
// Returns the userID, rememberMe flag, and any error (including expiry).
//
// Every rejection clears the cookie on the way out, the way the other sealed
// readers in this package do. Both TOTP cookies are session-scoped at path "/",
// so a value that cannot be opened, parsed, or honored is not merely useless —
// left in place it rides on every subsequent request for the rest of the
// browser session. The clear belongs on the read path rather than at the call
// sites because the reader is the only place that knows a value was presented
// and found unusable; a caller sees one error and would have to repeat the
// clear, and a later caller added without it silently reintroduces the leak.
// A missing value is the one branch that clears nothing: there is no value to
// retract, and an empty cookie is already the cleared state.
func (handler *Handler) parseTOTPPendingCookie(c fiber.Ctx) (uint, bool, error) {
	raw := strings.TrimSpace(c.Cookies(totpPendingCookieName))
	if raw == "" {
		return 0, false, errors.New("totp pending cookie missing")
	}

	codec, err := handler.cookieCodec()
	if err != nil {
		handler.clearTOTPPendingCookie(c)
		return 0, false, err
	}
	decoded, err := codec.open(totpPendingCookieName, raw)
	if err != nil {
		handler.clearTOTPPendingCookie(c)
		return 0, false, errors.New("totp pending cookie invalid")
	}

	var payload totpPendingCookiePayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		handler.clearTOTPPendingCookie(c)
		return 0, false, errors.New("totp pending cookie malformed")
	}
	if payload.UserID == 0 {
		handler.clearTOTPPendingCookie(c)
		return 0, false, errors.New("totp pending cookie missing user id")
	}
	if time.Now().After(payload.ExpiresAt) {
		handler.clearTOTPPendingCookie(c)
		return 0, false, errors.New("totp pending cookie expired")
	}

	return payload.UserID, payload.RememberMe, nil
}

// clearTOTPPendingCookie removes the TOTP pending cookie.
func (handler *Handler) clearTOTPPendingCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, totpPendingCookieSpec)
}

// setTOTPSetupCookie writes a short-lived sealed cookie that carries the raw
// TOTP secret during the enrollment flow (before the user has confirmed their
// code), scoped to the account the secret was generated for. A zero userID is a
// programming error (the caller enrolls an account it just resolved) and clears
// any prior cookie instead of sealing an unattributed payload. Refusing the zero
// id here is what keeps enrollment scoped structurally: once such a payload
// exists the confirm step has no account to compare against, and would enrol
// whatever secret the cookie carries against whichever session presented it.
func (handler *Handler) setTOTPSetupCookie(c fiber.Ctx, userID uint, rawSecret string) error {
	if userID == 0 {
		handler.clearTOTPSetupCookie(c)
		return errors.New("totp setup requires an owner id")
	}

	payload := totpSetupCookiePayload{
		UserID:    userID,
		RawSecret: rawSecret,
		ExpiresAt: time.Now().Add(totpPendingCookieTTL),
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Session-scoped (zero expires) — the payload carries its own ExpiresAt.
	return handler.writeSealedCookie(c, totpSetupCookieSpec, serialized, time.Time{})
}

// parseTOTPSetupCookie decodes and validates the TOTP setup cookie against the
// account presenting it. Returns the raw TOTP secret and any error (including a
// payload minted for a different account, one naming no account at all, and
// expiry).
//
// As on the pending cookie above, every rejection clears the cookie before
// returning; see that comment for why the clear lives on the read path. It
// matters more here: this payload carries the RAW TOTP secret of an enrollment
// nobody completed, so an abandoned or expired one left in place keeps that
// secret in transport on every later request of the session.
func (handler *Handler) parseTOTPSetupCookie(c fiber.Ctx, sessionUserID uint) (string, error) {
	raw := strings.TrimSpace(c.Cookies(totpSetupCookieName))
	if raw == "" {
		return "", errors.New("totp setup cookie missing")
	}

	codec, err := handler.cookieCodec()
	if err != nil {
		handler.clearTOTPSetupCookie(c)
		return "", err
	}
	decoded, err := codec.open(totpSetupCookieName, raw)
	if err != nil {
		handler.clearTOTPSetupCookie(c)
		return "", errors.New("totp setup cookie invalid")
	}

	var payload totpSetupCookiePayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		handler.clearTOTPSetupCookie(c)
		return "", errors.New("totp setup cookie malformed")
	}
	// The owner id sealed into the payload is never trusted alone: it only
	// counts when it matches the account presenting it, and a zero id on either
	// side names no account at all, so there is nothing to scope the pending
	// secret to. Both cases are invalid rather than a comparison that does not
	// apply. A cookie minted by a binary that predates this field lands here and
	// fails closed — the enrollment is restarted, not silently re-attributed.
	if !sealedPayloadBelongsToSession(payload.UserID, sessionUserID) {
		handler.clearTOTPSetupCookie(c)
		return "", errors.New("totp setup cookie does not belong to this session")
	}
	if strings.TrimSpace(payload.RawSecret) == "" {
		handler.clearTOTPSetupCookie(c)
		return "", errors.New("totp setup cookie missing secret")
	}
	if time.Now().After(payload.ExpiresAt) {
		handler.clearTOTPSetupCookie(c)
		return "", errors.New("totp setup cookie expired")
	}

	return payload.RawSecret, nil
}

// clearTOTPSetupCookie removes the TOTP setup cookie.
func (handler *Handler) clearTOTPSetupCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, totpSetupCookieSpec)
}
