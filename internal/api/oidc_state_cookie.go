package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
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

var oidcStateCookieSpec = sealedCookieSpec{
	name:        oidcStateCookieName,
	path:        security.OIDCCallbackPath,
	sameSite:    "None",
	forceSecure: true,
}

func (handler *Handler) setOIDCStateCookie(c fiber.Ctx, state oidcAuthState) error {
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
	return handler.writeSealedCookie(c, oidcStateCookieSpec, payload, time.Now().Add(oidcStateCookieTTL))
}

// peekOIDCStateCookie decodes the login state and consumes the cookie ONLY
// once that payload has proved valid — the same ordering peekOIDCStepupCookie
// documents: a stray hit on the callback path must not destroy a
// sign-in the owner is in the middle of.
//
// A value this reader REFUSES is retracted here, in the response that refused
// it, the way parseTOTPPendingCookie retracts the pending cookie. The two are
// not in tension: the peek-don't-pop rule protects a payload that could still
// be somebody's live flow, and a refused one never can be — setOIDCStateCookie
// mints nothing incomplete, and an expired payload is past the bound this
// reader itself enforces. Left in place it is simply re-sent to the callback
// path on every later request, and this cookie is SameSite=None, so any site
// can cause such a request. The clear belongs to the reader because the reader
// is the only place that knows a value was presented and found unusable; a
// caller sees an empty state and would have to repeat the clear, and the next
// caller added without it reintroduces the leak. A missing cookie is the one
// branch that retracts nothing: there is no value to retract, and an empty
// value is already the cleared state.
//
// The codec arm retracts for the same reason as the rest, not as a special
// case: cookieCodec() builds its codec under a sync.Once held on the Handler
// and caches the error there, so the scope of that cache is the Handler, not
// the process. The server composes one Handler (cmd/ovumcy), so within a
// running instance a codec that failed once fails for every later request and
// no flow this arm refuses can complete. A second Handler — which only tests
// build — would get its own attempt.
func (handler *Handler) peekOIDCStateCookie(c fiber.Ctx) oidcAuthState {
	raw := strings.TrimSpace(c.Cookies(oidcStateCookieName))
	if raw == "" {
		return oidcAuthState{}
	}

	codec, err := handler.cookieCodec()
	if err != nil {
		handler.clearOIDCStateCookie(c)
		return oidcAuthState{}
	}
	decoded, err := codec.open(oidcStateCookieName, raw)
	if err != nil {
		handler.clearOIDCStateCookie(c)
		return oidcAuthState{}
	}

	state := oidcAuthState{}
	if err := json.Unmarshal(decoded, &state); err != nil {
		handler.clearOIDCStateCookie(c)
		return oidcAuthState{}
	}
	if !state.validAt(time.Now()) {
		handler.clearOIDCStateCookie(c)
		return oidcAuthState{}
	}
	return state
}

func (handler *Handler) clearOIDCStateCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, oidcStateCookieSpec)
}
