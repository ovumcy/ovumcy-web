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

const oidcStateCookieTTL = 10 * time.Minute

type oidcAuthState struct {
	State        string `json:"state"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
	ExpiresAt    string `json:"expires_at"`
}

func newOIDCAuthState(now time.Time) (oidcAuthState, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state, err := security.RandomString(32, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	if err != nil {
		return oidcAuthState{}, err
	}
	nonce, err := security.RandomString(32, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	if err != nil {
		return oidcAuthState{}, err
	}
	return oidcAuthState{
		State:        state,
		Nonce:        nonce,
		CodeVerifier: oauth2.GenerateVerifier(),
		ExpiresAt:    now.UTC().Add(oidcStateCookieTTL).Format(time.RFC3339Nano),
	}, nil
}

func (state oidcAuthState) validAt(now time.Time) bool {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(state.ExpiresAt))
	if err != nil || !expiresAt.After(now.UTC()) {
		return false
	}
	return strings.TrimSpace(state.State) != "" &&
		strings.TrimSpace(state.Nonce) != "" &&
		strings.TrimSpace(state.CodeVerifier) != ""
}

func (state oidcAuthState) matchesState(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(state.State)), []byte(strings.TrimSpace(candidate))) == 1
}

func (handler *Handler) setOIDCStateCookie(c *fiber.Ctx, state oidcAuthState) error {
	if !handler.cookieSecure {
		return errors.New("oidc state cookie requires secure transport")
	}
	if !state.validAt(time.Now()) {
		return errors.New("oidc state cookie payload is required")
	}

	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	codec, err := handler.cookieCodec()
	if err != nil {
		return err
	}
	encoded, err := codec.seal(oidcStateCookieName, payload)
	if err != nil {
		return err
	}

	c.Cookie(&fiber.Cookie{
		Name:     oidcStateCookieName,
		Value:    encoded,
		Path:     security.OIDCCallbackPath,
		HTTPOnly: true,
		Secure:   true,
		SameSite: "None",
		Expires:  time.Now().Add(oidcStateCookieTTL),
	})
	return nil
}

func (handler *Handler) popOIDCStateCookie(c *fiber.Ctx) oidcAuthState {
	raw := strings.TrimSpace(c.Cookies(oidcStateCookieName))
	if raw == "" {
		return oidcAuthState{}
	}
	handler.clearOIDCStateCookie(c)

	codec, err := handler.cookieCodec()
	if err != nil {
		return oidcAuthState{}
	}
	decoded, err := codec.open(oidcStateCookieName, raw)
	if err != nil {
		return oidcAuthState{}
	}

	state := oidcAuthState{}
	if err := json.Unmarshal(decoded, &state); err != nil {
		return oidcAuthState{}
	}
	if !state.validAt(time.Now()) {
		return oidcAuthState{}
	}
	return state
}

func (handler *Handler) clearOIDCStateCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     oidcStateCookieName,
		Value:    "",
		Path:     security.OIDCCallbackPath,
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "None",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}
