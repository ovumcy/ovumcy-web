package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const recoveryCodeCookieTTL = 20 * time.Minute

const (
	recoveryCodeSurfaceDedicated      = "dedicated"
	recoveryCodeSurfaceInlineRegister = "inline_register"
)

const (
	recoveryCodeContinueTargetDashboard  = "dashboard"
	recoveryCodeContinueTargetOnboarding = "onboarding"
	recoveryCodeContinueTargetSettings   = "settings"
)

type recoveryCodeDisplayState struct {
	RecoveryCode   string
	ContinuePath   string
	ContinueTarget string
	Surface        string
}

type recoveryCodePagePayload struct {
	UserID         uint   `json:"uid"`
	RecoveryCode   string `json:"recovery_code"`
	ContinuePath   string `json:"continue_path,omitempty"`
	ContinueTarget string `json:"continue_target,omitempty"`
	Surface        string `json:"surface,omitempty"`
}

var recoveryCodeCookieSpec = sealedCookieSpec{name: recoveryCodeCookieName, path: "/"}

func (handler *Handler) setRecoveryCodeIssuanceCookie(c *fiber.Ctx, userID uint, recoveryCode string, continuePath string, surface string) error {
	code := strings.TrimSpace(recoveryCode)
	if code == "" {
		handler.clearRecoveryCodePageCookie(c)
		return errors.New("recovery code is required")
	}
	safeContinuePath := services.SanitizeRedirectPath(strings.TrimSpace(continuePath), "/dashboard")

	payload := recoveryCodePagePayload{
		UserID:         userID,
		RecoveryCode:   code,
		ContinuePath:   safeContinuePath,
		ContinueTarget: recoveryCodeContinueTargetFromPath(safeContinuePath),
		Surface:        sanitizeRecoveryCodeSurface(surface),
	}

	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return handler.writeSealedCookie(c, recoveryCodeCookieSpec, serialized, time.Now().Add(recoveryCodeCookieTTL))
}

func sanitizeRecoveryCodeContinueTarget(target string) string {
	switch strings.TrimSpace(target) {
	case recoveryCodeContinueTargetOnboarding:
		return recoveryCodeContinueTargetOnboarding
	case recoveryCodeContinueTargetSettings:
		return recoveryCodeContinueTargetSettings
	default:
		return recoveryCodeContinueTargetDashboard
	}
}

func recoveryCodeContinueTargetFromPath(path string) string {
	switch services.SanitizeRedirectPath(strings.TrimSpace(path), "/dashboard") {
	case "/onboarding":
		return recoveryCodeContinueTargetOnboarding
	case "/settings":
		return recoveryCodeContinueTargetSettings
	default:
		return recoveryCodeContinueTargetDashboard
	}
}

func recoveryCodeContinuePathFromTarget(target string) string {
	switch sanitizeRecoveryCodeContinueTarget(target) {
	case recoveryCodeContinueTargetOnboarding:
		return "/onboarding"
	case recoveryCodeContinueTargetSettings:
		return "/settings"
	default:
		return "/dashboard"
	}
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
	fallbackTarget := recoveryCodeContinueTargetFromPath(fallback)
	state := recoveryCodeDisplayState{
		ContinuePath:   fallback,
		ContinueTarget: fallbackTarget,
		Surface:        recoveryCodeSurfaceDedicated,
	}

	raw := strings.TrimSpace(c.Cookies(recoveryCodeCookieName))
	if raw == "" {
		return state
	}

	codec, err := handler.cookieCodec()
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

	continueTarget := strings.TrimSpace(payload.ContinueTarget)
	if continueTarget == "" {
		continueTarget = recoveryCodeContinueTargetFromPath(payload.ContinuePath)
	} else {
		continueTarget = sanitizeRecoveryCodeContinueTarget(continueTarget)
	}

	state.RecoveryCode = code
	state.ContinueTarget = continueTarget
	state.ContinuePath = recoveryCodeContinuePathFromTarget(continueTarget)
	state.Surface = sanitizeRecoveryCodeSurface(payload.Surface)
	return state
}

func (handler *Handler) clearRecoveryCodePageCookie(c *fiber.Ctx) {
	handler.clearSealedCookie(c, recoveryCodeCookieSpec)
}
