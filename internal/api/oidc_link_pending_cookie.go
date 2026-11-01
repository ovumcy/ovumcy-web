package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The identity-link step-up callback no longer writes the link (WEB-401). It
// parks the identity the provider returned in this sealed cookie, bound to the
// account and session version that started the step-up, and the owner confirms
// it on a same-origin page; only that CSRF-protected confirm writes the link.
// The callback is CSRF-exempt and can be driven cross-site with an attacker's
// own authorization code, so without this stop the attacker's identity would be
// linked to the owner's account on arrival.

const oidcLinkPendingCookieTTL = 5 * time.Minute

type oidcLinkPendingCookiePayload struct {
	UserID uint `json:"user_id"`
	// SessionVersion is the version the step-up's factors were verified
	// against; the link is written only from it.
	SessionVersion int       `json:"sv"`
	Issuer         string    `json:"iss"`
	Subject        string    `json:"sub"`
	Email          string    `json:"email,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
}

func (payload oidcLinkPendingCookiePayload) claims() security.OIDCClaims {
	return security.OIDCClaims{Issuer: payload.Issuer, Subject: payload.Subject, Email: payload.Email}
}

// Named ovumcy_oidc_link_confirm, not the retired ovumcy_oidc_link_pending
// (WEB-77): a fresh purpose string keeps any value an older binary sealed under
// that name from opening here.
var oidcLinkPendingCookieSpec = sealedCookieSpec{name: oidcLinkPendingCookieName, path: "/"}

func (handler *Handler) setOIDCLinkPendingCookie(c fiber.Ctx, userID uint, sessionVersion int, claims security.OIDCClaims) error {
	if userID == 0 {
		handler.clearOIDCLinkPendingCookie(c)
		return errors.New("oidc link pending cookie requires an owner id")
	}
	if strings.TrimSpace(claims.Issuer) == "" || strings.TrimSpace(claims.Subject) == "" {
		handler.clearOIDCLinkPendingCookie(c)
		return errors.New("oidc link pending cookie requires an identity")
	}
	payload := oidcLinkPendingCookiePayload{
		UserID:         userID,
		SessionVersion: services.NormalizeAuthSessionVersion(sessionVersion),
		Issuer:         claims.Issuer,
		Subject:        claims.Subject,
		Email:          verifiedEmail(claims),
		ExpiresAt:      time.Now().Add(oidcLinkPendingCookieTTL),
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// Session-scoped (zero expires) — the payload carries its own ExpiresAt.
	return handler.writeSealedCookie(c, oidcLinkPendingCookieSpec, serialized, time.Time{})
}

// verifiedEmail is the email the confirmation page may name. An unverified
// claim is whatever the provider account's holder typed — on a provider with
// self-registration, the owner's own address — so it is never shown; the page
// names the subject instead.
func verifiedEmail(claims security.OIDCClaims) string {
	if !claims.EmailVerified {
		return ""
	}
	return strings.TrimSpace(claims.Email)
}

// parseOIDCLinkPendingCookie returns the pending link only when it was minted
// for the account presenting it and has not expired. Every rejection clears the
// cookie on the way out, as the TOTP cookie readers do.
func (handler *Handler) parseOIDCLinkPendingCookie(c fiber.Ctx, sessionUserID uint) (oidcLinkPendingCookiePayload, error) {
	raw := strings.TrimSpace(c.Cookies(oidcLinkPendingCookieName))
	if raw == "" {
		return oidcLinkPendingCookiePayload{}, errors.New("oidc link pending cookie missing")
	}

	codec, err := handler.cookieCodec()
	if err != nil {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingCookiePayload{}, err
	}
	decoded, err := codec.open(oidcLinkPendingCookieName, raw)
	if err != nil {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingCookiePayload{}, errors.New("oidc link pending cookie invalid")
	}

	var payload oidcLinkPendingCookiePayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingCookiePayload{}, errors.New("oidc link pending cookie malformed")
	}
	if !sealedPayloadBelongsToSession(payload.UserID, sessionUserID) {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingCookiePayload{}, errors.New("oidc link pending cookie does not belong to this session")
	}
	if payload.SessionVersion < 1 {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingCookiePayload{}, errors.New("oidc link pending cookie missing session version")
	}
	if strings.TrimSpace(payload.Issuer) == "" || strings.TrimSpace(payload.Subject) == "" {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingCookiePayload{}, errors.New("oidc link pending cookie missing identity")
	}
	if time.Now().After(payload.ExpiresAt) {
		handler.clearOIDCLinkPendingCookie(c)
		return oidcLinkPendingCookiePayload{}, errors.New("oidc link pending cookie expired")
	}
	return payload, nil
}

func (handler *Handler) clearOIDCLinkPendingCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, oidcLinkPendingCookieSpec)
}
