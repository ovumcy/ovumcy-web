package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// recoveryCodeCookieTTL is the reveal's lifetime, written from one value into
// two places: the Set-Cookie `Expires` attribute and the `expires_at` the
// sealed payload carries. Only the second is a bound. The attribute is a hint
// the browser is free to ignore, so a client that keeps the sealed value can
// hand it back afterwards — a reveal cookie without a server-verified expiry
// stays replayable until the code is rotated or SECRET_KEY changes.
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
	// ExpiresAt is the server-verified half of the reveal's lifetime, carried
	// the way the TOTP enrollment cookie carries its own beside its owner id:
	// the payload names the moment it stops being honored, and the reader
	// compares that against the clock instead of trusting the browser to have
	// dropped the cookie. A payload from before the field existed decodes to the
	// zero time, which is always already past, so an absent bound is invalid
	// input rather than permission to reveal for as long as the code lives.
	ExpiresAt time.Time `json:"expires_at"`
}

var recoveryCodeCookieSpec = sealedCookieSpec{name: recoveryCodeCookieName, path: "/"}

// setRecoveryCodeIssuanceCookie seals a freshly minted recovery code for a
// one-time reveal, scoped to userID so the code is only ever shown back to the
// account it was minted for. A zero userID or an empty code is a programming
// error and clears any prior cookie instead of sealing an unattributed or blank
// payload. Refusing the zero id here is what keeps the reveal scoped
// structurally: the read path has no owner to compare against once such a
// payload exists.
func (handler *Handler) setRecoveryCodeIssuanceCookie(c fiber.Ctx, userID uint, recoveryCode string, continuePath string, surface string) error {
	if userID == 0 {
		handler.clearRecoveryCodePageCookie(c)
		return errors.New("recovery code reveal requires an owner id")
	}
	code := strings.TrimSpace(recoveryCode)
	if code == "" {
		handler.clearRecoveryCodePageCookie(c)
		return errors.New("recovery code is required")
	}
	safeContinuePath := services.SanitizeRedirectPath(strings.TrimSpace(continuePath), "/dashboard")
	expiresAt := time.Now().Add(recoveryCodeCookieTTL)

	payload := recoveryCodePagePayload{
		UserID:         userID,
		RecoveryCode:   code,
		ContinuePath:   safeContinuePath,
		ContinueTarget: recoveryCodeContinueTargetFromPath(safeContinuePath),
		Surface:        sanitizeRecoveryCodeSurface(surface),
		ExpiresAt:      expiresAt,
	}

	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return handler.writeSealedCookie(c, recoveryCodeCookieSpec, serialized, expiresAt)
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

// readRecoveryCodeDisplayState opens the sealed one-time cookie and returns the
// code to display, or the code-less fallback state when the cookie is absent,
// malformed, unattributed, scoped to a different account, or past the expiry it
// carries — including a payload that carries none. Every rejection path clears
// the cookie so an unusable value cannot linger.
func (handler *Handler) readRecoveryCodeDisplayState(c fiber.Ctx, userID uint, fallbackContinuePath string) recoveryCodeDisplayState {
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
	if !sealedPayloadBelongsToSession(payload.UserID, userID) {
		handler.clearRecoveryCodePageCookie(c)
		return state
	}
	// Refusing here takes away the display, never the sign-in: the page already
	// resolved an authenticated session, so the owner lands on the same continue
	// path they reach when the cookie is simply absent, and regenerates the code
	// from Settings.
	if time.Now().After(payload.ExpiresAt) {
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

func (handler *Handler) clearRecoveryCodePageCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, recoveryCodeCookieSpec)
}
