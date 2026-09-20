package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// OIDC link-pending cookie. Issued by the OIDC callback when an authenticated
// exchange resolved to a pre-existing local user by email but the (issuer,
// subject) pair has never been linked to that user. Auto-linking in that
// situation would let a malicious or sloppy upstream IdP take over the account
// by asserting a verified email it does not actually control; instead the
// callback hands the user off to a password-confirmation step at
// /auth/oidc/link-confirm, which validates the holder of the target account
// before invoking ConfirmAndLinkIdentity.
//
// The cookie carries the OIDC claims plus the target user id so the
// confirmation handler can run completely off cookie state without re-running
// the OIDC exchange. AAD-by-name on the secure-cookie codec stops cross-cookie
// substitution; the short TTL bounds the replay window if the cookie ever
// leaks.

const oidcLinkPendingCookieTTL = 5 * time.Minute

const oidcLinkConfirmPath = "/auth/oidc/link-confirm"

type oidcLinkPendingPayload struct {
	TargetUserID uint   `json:"target_user_id"`
	Issuer       string `json:"issuer"`
	Subject      string `json:"subject"`
	Email        string `json:"email"`
	ExpiresAt    string `json:"expires_at"`
}

func newOIDCLinkPendingPayload(now time.Time, targetUserID uint, issuer, subject, email string) (oidcLinkPendingPayload, error) {
	if targetUserID == 0 {
		return oidcLinkPendingPayload{}, errors.New("oidc link pending requires target user id")
	}
	if strings.TrimSpace(issuer) == "" || strings.TrimSpace(subject) == "" {
		return oidcLinkPendingPayload{}, errors.New("oidc link pending requires issuer and subject")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return oidcLinkPendingPayload{
		TargetUserID: targetUserID,
		Issuer:       strings.TrimSpace(issuer),
		Subject:      strings.TrimSpace(subject),
		Email:        strings.TrimSpace(email),
		ExpiresAt:    now.UTC().Add(oidcLinkPendingCookieTTL).Format(time.RFC3339Nano),
	}, nil
}

func (p oidcLinkPendingPayload) validAt(now time.Time) bool {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(p.ExpiresAt))
	if err != nil || !expiresAt.After(now.UTC()) {
		return false
	}
	return p.TargetUserID != 0 &&
		strings.TrimSpace(p.Issuer) != "" &&
		strings.TrimSpace(p.Subject) != ""
}

var oidcLinkPendingCookieSpec = sealedCookieSpec{name: oidcLinkPendingCookieName, path: oidcLinkConfirmPath}

func (handler *Handler) setOIDCLinkPendingCookie(c fiber.Ctx, payload oidcLinkPendingPayload) error {
	if !payload.validAt(time.Now()) {
		return errors.New("oidc link pending payload is required")
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return handler.writeSealedCookie(c, oidcLinkPendingCookieSpec, serialized, time.Now().Add(oidcLinkPendingCookieTTL))
}

// readOIDCLinkPendingCookie returns the pending link the confirmation page
// acts on, and retracts any value it refuses in the response that refused it —
// the convention every sealed reader in this package follows, pinned by
// TestEverySealedCookieReaderRetractsTheValueItRefuses.
//
// Nothing that reaches a refusal arm can be a live hand-off:
// setOIDCLinkPendingCookie mints no payload without a target user, an issuer
// and a subject, and an expired one is past the five-minute bound this reader
// itself enforces. Left riding, it is re-sent to the confirmation path on every
// later request there, carrying the target user id, the issuer, the subject and
// the asserted email of an account the server has already said it will not
// link. The clear belongs here because the reader is the only place that knows
// a value was presented and found unusable; both handlers see an empty payload
// and would have to repeat the clear, and the next caller added without it
// reintroduces the leak. A missing cookie retracts nothing: there is no value
// to retract, and an empty value is already the cleared state.
//
// A payload this reader HONOURS is left in place, for the handler that
// completes the confirmation to spend.
//
// The codec arm retracts for the same reason as the rest: cookieCodec() builds
// its codec under a sync.Once held on the Handler and caches the error there,
// and the server composes one Handler for the process (cmd/ovumcy), so within
// a running instance a codec that failed once fails for every later request
// and no flow this arm refuses can complete.
func (handler *Handler) readOIDCLinkPendingCookie(c fiber.Ctx) (oidcLinkPendingPayload, bool) {
	raw := strings.TrimSpace(c.Cookies(oidcLinkPendingCookieName))
	if raw == "" {
		return oidcLinkPendingPayload{}, false
	}
	codec, err := handler.cookieCodec()
	if err != nil {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingPayload{}, false
	}
	decoded, err := codec.open(oidcLinkPendingCookieName, raw)
	if err != nil {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingPayload{}, false
	}
	payload := oidcLinkPendingPayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingPayload{}, false
	}
	if !payload.validAt(time.Now()) {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingPayload{}, false
	}
	return payload, true
}

func (handler *Handler) clearOIDCLinkPendingCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, oidcLinkPendingCookieSpec)
}
