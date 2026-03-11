package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

const recoveryCodeCookieTTL = 20 * time.Minute

const (
	recoveryCodeSurfaceDedicated      = "dedicated"
	recoveryCodeSurfaceInlineRegister = "inline_register"
)

type recoveryCodeDisplayState struct {
	RecoveryCode string
	ContinuePath string
	Surface      string
}

type recoveryCodePagePayload struct {
	UserID       uint   `json:"uid"`
	RecoveryCode string `json:"recovery_code"`
	ContinuePath string `json:"continue_path,omitempty"`
	Surface      string `json:"surface,omitempty"`
}

func (handler *Handler) setRecoveryCodeIssuanceCookie(c *fiber.Ctx, userID uint, recoveryCode string, continuePath string, surface string) error {
	code := strings.TrimSpace(recoveryCode)
	if code == "" {
		handler.clearRecoveryCodePageCookie(c)
		return errors.New("recovery code is required")
	}

	payload := recoveryCodePagePayload{
		UserID:       userID,
		RecoveryCode: code,
		ContinuePath: services.SanitizeRedirectPath(strings.TrimSpace(continuePath), "/dashboard"),
		Surface:      sanitizeRecoveryCodeSurface(surface),
	}

	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return err
	}
	encoded, err := codec.seal(recoveryCodeCookieName, serialized)
	if err != nil {
		return err
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
	return nil
}

func sanitizeRecoveryCodeSurface(surface string) string {
	switch strings.TrimSpace(surface) {
	case recoveryCodeSurfaceInlineRegister:
		return recoveryCodeSurfaceInlineRegister
	default:
		return recoveryCodeSurfaceDedicated
	}
}

func (handler *Handler) readRecoveryCodeDisplayState(c *fiber.Ctx, userID uint, fallbackContinuePath string) recoveryCodeDisplayState {
	fallback := services.SanitizeRedirectPath(strings.TrimSpace(fallbackContinuePath), "/dashboard")
	state := recoveryCodeDisplayState{
		ContinuePath: fallback,
		Surface:      recoveryCodeSurfaceDedicated,
	}

	raw := strings.TrimSpace(c.Cookies(recoveryCodeCookieName))
	if raw == "" {
		return state
	}

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		handler.clearRecoveryCodePageCookie(c)
		return state
	}

	decoded, err := codec.open(recoveryCodeCookieName, raw)
	if err != nil {
		handler.clearRecoveryCodePageCookie(c)
		return state
	}

	payload := recoveryCodePagePayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		handler.clearRecoveryCodePageCookie(c)
		return state
	}

	code := strings.TrimSpace(payload.RecoveryCode)
	if code == "" {
		handler.clearRecoveryCodePageCookie(c)
		return state
	}
	if payload.UserID != 0 && userID != 0 && payload.UserID != userID {
		handler.clearRecoveryCodePageCookie(c)
		return state
	}

	state.RecoveryCode = code
	state.ContinuePath = services.SanitizeRedirectPath(strings.TrimSpace(payload.ContinuePath), fallback)
	state.Surface = sanitizeRecoveryCodeSurface(payload.Surface)
	return state
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
