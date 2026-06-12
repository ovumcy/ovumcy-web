package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"golang.org/x/oauth2"
)

// OIDC step-up cookie. Carries the state needed to complete a fresh-reauth
// callback for an already-signed-in user (currently: enabling a local
// password on an OIDC-only account). Distinct from oidcStateCookieName so
// the callback handler can detect step-up vs ordinary login without
// ambiguity.

const oidcStepupCookieTTL = 10 * time.Minute

// oidcStepupPurpose enumerates the actions a step-up reauth can complete.
// Encoded into the cookie so the callback handler dispatches to the right
// completion handler and refuses callbacks aimed at a different purpose.
type oidcStepupPurpose string

const oidcStepupPurposeLocalPasswordSetup oidcStepupPurpose = "local_password_setup"

type oidcStepupState struct {
	Purpose      oidcStepupPurpose `json:"purpose"`
	UserID       uint              `json:"user_id"`
	State        string            `json:"state"`
	Nonce        string            `json:"nonce"`
	CodeVerifier string            `json:"code_verifier"`
	PasswordHash string            `json:"password_hash"`
	ExpiresAt    string            `json:"expires_at"`
}

func newOIDCStepupState(now time.Time, purpose oidcStepupPurpose, userID uint, passwordHash string) (oidcStepupState, error) {
	if userID == 0 {
		return oidcStepupState{}, errors.New("oidc stepup state requires user id")
	}
	if strings.TrimSpace(passwordHash) == "" {
		return oidcStepupState{}, errors.New("oidc stepup state requires password hash")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state, err := security.RandomString(32, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	if err != nil {
		return oidcStepupState{}, err
	}
	nonce, err := security.RandomString(32, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	if err != nil {
		return oidcStepupState{}, err
	}
	return oidcStepupState{
		Purpose:      purpose,
		UserID:       userID,
		State:        state,
		Nonce:        nonce,
		CodeVerifier: oauth2.GenerateVerifier(),
		PasswordHash: passwordHash,
		ExpiresAt:    now.UTC().Add(oidcStepupCookieTTL).Format(time.RFC3339Nano),
	}, nil
}

func (state oidcStepupState) validAt(now time.Time) bool {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(state.ExpiresAt))
	if err != nil || !expiresAt.After(now.UTC()) {
		return false
	}
	return state.Purpose != "" &&
		state.UserID != 0 &&
		strings.TrimSpace(state.State) != "" &&
		strings.TrimSpace(state.Nonce) != "" &&
		strings.TrimSpace(state.CodeVerifier) != "" &&
		strings.TrimSpace(state.PasswordHash) != ""
}

func (state oidcStepupState) matchesState(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(state.State)), []byte(strings.TrimSpace(candidate))) == 1
}

var oidcStepupCookieSpec = sealedCookieSpec{
	name:        oidcStepupCookieName,
	path:        security.OIDCCallbackPath,
	sameSite:    "None",
	forceSecure: true,
}

func (handler *Handler) setOIDCStepupCookie(c *fiber.Ctx, state oidcStepupState) error {
	if !handler.cookieSecure {
		return errors.New("oidc stepup cookie requires secure transport")
	}
	if !state.validAt(time.Now()) {
		return errors.New("oidc stepup cookie payload is required")
	}

	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return handler.writeSealedCookie(c, oidcStepupCookieSpec, payload, time.Now().Add(oidcStepupCookieTTL))
}

func (handler *Handler) popOIDCStepupCookie(c *fiber.Ctx) oidcStepupState {
	raw := strings.TrimSpace(c.Cookies(oidcStepupCookieName))
	if raw == "" {
		return oidcStepupState{}
	}
	handler.clearOIDCStepupCookie(c)

	codec, err := handler.cookieCodec()
	if err != nil {
		return oidcStepupState{}
	}
	decoded, err := codec.open(oidcStepupCookieName, raw)
	if err != nil {
		return oidcStepupState{}
	}

	state := oidcStepupState{}
	if err := json.Unmarshal(decoded, &state); err != nil {
		return oidcStepupState{}
	}
	if !state.validAt(time.Now()) {
		return oidcStepupState{}
	}
	return state
}

func (handler *Handler) clearOIDCStepupCookie(c *fiber.Ctx) {
	handler.clearSealedCookie(c, oidcStepupCookieSpec)
}
