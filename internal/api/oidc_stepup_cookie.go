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

const (
	oidcStepupPurposeLocalPasswordSetup oidcStepupPurpose = "local_password_setup"
	// oidcStepupPurposeErasure gates clear-data and account deletion for an
	// account that has no local password to confirm with. The re-auth
	// requirement itself is unchanged — an erasure still costs a fresh
	// authentication — but for an OIDC-only owner the credential that proves
	// possession lives at the provider, so the same step-up primitive stands in
	// for the password prompt. An account that HAS a local password never
	// reaches this purpose: its gate stays the password.
	oidcStepupPurposeErasure oidcStepupPurpose = "erasure"
	// oidcStepupPurposeIdentityLink gates linking a NEW OIDC identity to the
	// currently authenticated account from Settings (issue #701). Linking is a
	// permanent, password-change-weight binding, so it is authorised the same
	// way as the other step-ups here: a fresh interactive provider
	// authentication, never the public unauthenticated link-confirm route.
	oidcStepupPurposeIdentityLink oidcStepupPurpose = "identity_link"
)

// oidcStepupErasureOperation names which erasure the step-up was started for.
// It is carried in the sealed state rather than re-read from the callback
// request because the callback arrives from the provider with no body of its
// own: whatever the owner confirmed before leaving is what must execute on
// return, and nothing observable in between may redirect it to the other,
// more destructive operation.
type oidcStepupErasureOperation string

const (
	oidcStepupErasureClearData     oidcStepupErasureOperation = "clear_data"
	oidcStepupErasureDeleteAccount oidcStepupErasureOperation = "delete_account"
)

func (operation oidcStepupErasureOperation) valid() bool {
	return operation == oidcStepupErasureClearData || operation == oidcStepupErasureDeleteAccount
}

type oidcStepupState struct {
	Purpose      oidcStepupPurpose          `json:"purpose"`
	UserID       uint                       `json:"user_id"`
	State        string                     `json:"state"`
	Nonce        string                     `json:"nonce"`
	CodeVerifier string                     `json:"code_verifier"`
	PasswordHash string                     `json:"password_hash"`
	Operation    oidcStepupErasureOperation `json:"operation,omitempty"`
	ExpiresAt    string                     `json:"expires_at"`
}

// newOIDCStepupState builds the local-password-setup state, which carries the
// prepared password hash the callback commits.
func newOIDCStepupState(now time.Time, purpose oidcStepupPurpose, userID uint, passwordHash string) (oidcStepupState, error) {
	if strings.TrimSpace(passwordHash) == "" {
		return oidcStepupState{}, errors.New("oidc stepup state requires password hash")
	}
	return newOIDCStepupStateForPurpose(now, purpose, userID, passwordHash, "")
}

// newOIDCErasureStepupState builds the erasure state. It carries no password
// hash — there is no credential to commit, only an operation to execute once
// the provider has re-authenticated the owner.
func newOIDCErasureStepupState(now time.Time, userID uint, operation oidcStepupErasureOperation) (oidcStepupState, error) {
	if !operation.valid() {
		return oidcStepupState{}, errors.New("oidc stepup state requires a known erasure operation")
	}
	return newOIDCStepupStateForPurpose(now, oidcStepupPurposeErasure, userID, "", operation)
}

// newOIDCIdentityLinkStepupState builds the identity-link state. Like erasure
// it carries no password hash and no operation — the callback's only job is to
// prove a fresh interactive authentication before ConfirmAndLinkIdentity runs
// against whatever (issuer, subject) the exchange returns.
func newOIDCIdentityLinkStepupState(now time.Time, userID uint) (oidcStepupState, error) {
	return newOIDCStepupStateForPurpose(now, oidcStepupPurposeIdentityLink, userID, "", "")
}

func newOIDCStepupStateForPurpose(now time.Time, purpose oidcStepupPurpose, userID uint, passwordHash string, operation oidcStepupErasureOperation) (oidcStepupState, error) {
	if userID == 0 {
		return oidcStepupState{}, errors.New("oidc stepup state requires user id")
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
	built := oidcStepupState{
		Purpose:      purpose,
		UserID:       userID,
		State:        state,
		Nonce:        nonce,
		CodeVerifier: oauth2.GenerateVerifier(),
		PasswordHash: passwordHash,
		Operation:    operation,
		ExpiresAt:    now.UTC().Add(oidcStepupCookieTTL).Format(time.RFC3339Nano),
	}
	if !built.payloadCompleteForPurpose() {
		// codecov:ignore:start -- both constructors validate their own purpose's
		// payload above, so this is a guard against a future third purpose being
		// added without teaching payloadCompleteForPurpose about it.
		return oidcStepupState{}, errors.New("oidc stepup state payload is incomplete for its purpose")
		// codecov:ignore:end
	}
	return built, nil
}

// payloadCompleteForPurpose reports whether the state carries exactly the
// fields its purpose needs. The per-purpose arms are mutually exclusive by
// construction: a password-setup payload carries a prepared hash and no
// operation, an erasure payload carries an operation and no hash. Requiring the
// absence as well as the presence is what keeps the two shapes from being
// reinterpreted as each other, and an UNKNOWN purpose satisfies neither arm —
// so a payload minted by a future or foreign binary fails closed here rather
// than inheriting whichever arm happened to be listed first.
func (state oidcStepupState) payloadCompleteForPurpose() bool {
	if state.UserID == 0 ||
		strings.TrimSpace(state.State) == "" ||
		strings.TrimSpace(state.Nonce) == "" ||
		strings.TrimSpace(state.CodeVerifier) == "" {
		return false
	}
	switch state.Purpose {
	case oidcStepupPurposeLocalPasswordSetup:
		return strings.TrimSpace(state.PasswordHash) != "" && state.Operation == ""
	case oidcStepupPurposeErasure:
		return state.Operation.valid() && strings.TrimSpace(state.PasswordHash) == ""
	case oidcStepupPurposeIdentityLink:
		return state.Operation == "" && strings.TrimSpace(state.PasswordHash) == ""
	default:
		return false
	}
}

func (state oidcStepupState) validAt(now time.Time) bool {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(state.ExpiresAt))
	if err != nil || !expiresAt.After(now.UTC()) {
		return false
	}
	return state.payloadCompleteForPurpose()
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

func (handler *Handler) setOIDCStepupCookie(c fiber.Ctx, state oidcStepupState) error {
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

func (handler *Handler) popOIDCStepupCookie(c fiber.Ctx) oidcStepupState {
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

func (handler *Handler) clearOIDCStepupCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, oidcStepupCookieSpec)
}
