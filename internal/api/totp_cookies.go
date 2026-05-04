package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const totpPendingCookieTTL = 5 * time.Minute

type totpPendingCookiePayload struct {
	UserID     uint      `json:"user_id"`
	RememberMe bool      `json:"remember_me,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type totpSetupCookiePayload struct {
	RawSecret string    `json:"raw_secret"`
	ExpiresAt time.Time `json:"expires_at"`
}

// setTOTPPendingCookie writes a short-lived sealed cookie that carries the user's
// ID and rememberMe flag across the 2FA challenge step.
func (handler *Handler) setTOTPPendingCookie(c *fiber.Ctx, userID uint, rememberMe bool) error {
	payload := totpPendingCookiePayload{
		UserID:     userID,
		RememberMe: rememberMe,
		ExpiresAt:  time.Now().Add(totpPendingCookieTTL),
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return err
	}
	encoded, err := codec.seal(totpPendingCookieName, serialized)
	if err != nil {
		return err
	}

	c.Cookie(&fiber.Cookie{
		Name:     totpPendingCookieName,
		Value:    encoded,
		Path:     "/",
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		// Session-scoped (no Expires) — the payload carries its own ExpiresAt.
	})
	return nil
}

// parseTOTPPendingCookie decodes and validates the TOTP pending cookie.
// Returns the userID, rememberMe flag, and any error (including expiry).
func (handler *Handler) parseTOTPPendingCookie(c *fiber.Ctx) (uint, bool, error) {
	raw := strings.TrimSpace(c.Cookies(totpPendingCookieName))
	if raw == "" {
		return 0, false, errors.New("totp pending cookie missing")
	}

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return 0, false, err
	}
	decoded, err := codec.open(totpPendingCookieName, raw)
	if err != nil {
		return 0, false, errors.New("totp pending cookie invalid")
	}

	var payload totpPendingCookiePayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return 0, false, errors.New("totp pending cookie malformed")
	}
	if payload.UserID == 0 {
		return 0, false, errors.New("totp pending cookie missing user id")
	}
	if time.Now().After(payload.ExpiresAt) {
		return 0, false, errors.New("totp pending cookie expired")
	}

	return payload.UserID, payload.RememberMe, nil
}

// clearTOTPPendingCookie removes the TOTP pending cookie.
func (handler *Handler) clearTOTPPendingCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     totpPendingCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}

// setTOTPSetupCookie writes a short-lived sealed cookie that carries the raw
// TOTP secret during the enrollment flow (before the user has confirmed their code).
func (handler *Handler) setTOTPSetupCookie(c *fiber.Ctx, rawSecret string) error {
	payload := totpSetupCookiePayload{
		RawSecret: rawSecret,
		ExpiresAt: time.Now().Add(totpPendingCookieTTL),
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return err
	}
	encoded, err := codec.seal(totpSetupCookieName, serialized)
	if err != nil {
		return err
	}

	c.Cookie(&fiber.Cookie{
		Name:     totpSetupCookieName,
		Value:    encoded,
		Path:     "/",
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
	})
	return nil
}

// parseTOTPSetupCookie decodes and validates the TOTP setup cookie.
// Returns the raw TOTP secret and any error (including expiry).
func (handler *Handler) parseTOTPSetupCookie(c *fiber.Ctx) (string, error) {
	raw := strings.TrimSpace(c.Cookies(totpSetupCookieName))
	if raw == "" {
		return "", errors.New("totp setup cookie missing")
	}

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return "", err
	}
	decoded, err := codec.open(totpSetupCookieName, raw)
	if err != nil {
		return "", errors.New("totp setup cookie invalid")
	}

	var payload totpSetupCookiePayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return "", errors.New("totp setup cookie malformed")
	}
	if strings.TrimSpace(payload.RawSecret) == "" {
		return "", errors.New("totp setup cookie missing secret")
	}
	if time.Now().After(payload.ExpiresAt) {
		return "", errors.New("totp setup cookie expired")
	}

	return payload.RawSecret, nil
}

// clearTOTPSetupCookie removes the TOTP setup cookie.
func (handler *Handler) clearTOTPSetupCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     totpSetupCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}
