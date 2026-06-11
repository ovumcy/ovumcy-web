package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
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

func (handler *Handler) setOIDCLinkPendingCookie(c *fiber.Ctx, payload oidcLinkPendingPayload) error {
	if !payload.validAt(time.Now()) {
		return errors.New("oidc link pending payload is required")
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	codec, err := handler.cookieCodec()
	if err != nil {
		return err
	}
	encoded, err := codec.seal(oidcLinkPendingCookieName, serialized)
	if err != nil {
		return err
	}

	c.Cookie(&fiber.Cookie{
		Name:     oidcLinkPendingCookieName,
		Value:    encoded,
		Path:     oidcLinkConfirmPath,
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(oidcLinkPendingCookieTTL),
	})
	return nil
}

func (handler *Handler) readOIDCLinkPendingCookie(c *fiber.Ctx) (oidcLinkPendingPayload, bool) {
	raw := strings.TrimSpace(c.Cookies(oidcLinkPendingCookieName))
	if raw == "" {
		return oidcLinkPendingPayload{}, false
	}
	codec, err := handler.cookieCodec()
	if err != nil {
		return oidcLinkPendingPayload{}, false
	}
	decoded, err := codec.open(oidcLinkPendingCookieName, raw)
	if err != nil {
		return oidcLinkPendingPayload{}, false
	}
	payload := oidcLinkPendingPayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return oidcLinkPendingPayload{}, false
	}
	if !payload.validAt(time.Now()) {
		return oidcLinkPendingPayload{}, false
	}
	return payload, true
}

func (handler *Handler) clearOIDCLinkPendingCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     oidcLinkPendingCookieName,
		Value:    "",
		Path:     oidcLinkConfirmPath,
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}
