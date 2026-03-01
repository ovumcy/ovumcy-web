package api

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

const recoveryCodeCookieTTL = 20 * time.Minute

type recoveryCodePagePayload struct {
	UserID       uint   `json:"uid"`
	RecoveryCode string `json:"recovery_code"`
	ContinuePath string `json:"continue_path,omitempty"`
}

func (handler *Handler) setRecoveryCodePageCookie(c *fiber.Ctx, userID uint, recoveryCode string, continuePath string) {
	code := strings.TrimSpace(recoveryCode)
	if code == "" {
		handler.clearRecoveryCodePageCookie(c)
		return
	}

	payload := recoveryCodePagePayload{
		UserID:       userID,
		RecoveryCode: code,
		ContinuePath: services.SanitizeRedirectPath(strings.TrimSpace(continuePath), "/dashboard"),
	}

	serialized, err := json.Marshal(payload)
	if err != nil {
		return
	}
	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return
	}
	encoded, err := codec.seal(recoveryCodeCookieName, serialized)
	if err != nil {
		return
	}

	c.Cookie(&fiber.Cookie{
		Name:     recoveryCodeCookieName,
		Value:    encoded,
		Path:     "/",
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(recoveryCodeCookieTTL),
	})
}

func (handler *Handler) readRecoveryCodePageCookie(c *fiber.Ctx, userID uint, fallbackContinuePath string) (string, string) {
	fallback := services.SanitizeRedirectPath(strings.TrimSpace(fallbackContinuePath), "/dashboard")
	raw := strings.TrimSpace(c.Cookies(recoveryCodeCookieName))
	if raw == "" {
		return "", fallback
	}

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		handler.clearRecoveryCodePageCookie(c)
		return "", fallback
	}

	decoded, err := codec.open(recoveryCodeCookieName, raw)
	if err != nil {
		handler.clearRecoveryCodePageCookie(c)
		return "", fallback
	}

	payload := recoveryCodePagePayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		handler.clearRecoveryCodePageCookie(c)
		return "", fallback
	}

	code := strings.TrimSpace(payload.RecoveryCode)
	if code == "" {
		handler.clearRecoveryCodePageCookie(c)
		return "", fallback
	}
	if payload.UserID != 0 && userID != 0 && payload.UserID != userID {
		handler.clearRecoveryCodePageCookie(c)
		return "", fallback
	}

	continuePath := services.SanitizeRedirectPath(strings.TrimSpace(payload.ContinuePath), fallback)
	return code, continuePath
}

func (handler *Handler) clearRecoveryCodePageCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     recoveryCodeCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}
