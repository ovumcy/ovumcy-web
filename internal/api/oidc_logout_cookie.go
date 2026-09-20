package api

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

var oidcLogoutBridgeCookieSpec = sealedCookieSpec{name: oidcLogoutBridgeCookieName, path: oidcLogoutBridgePath}

// oidcLogoutBridgeCookiePayload is what the provider-logout bridge resolves
// from. The bridge runs after the auth cookie is gone, so this payload is the
// only thing naming the account the hop acts for: it carries the owner beside
// the session id, and the pair — never the session id alone — is what reads the
// stored end-session material (`docs/SECURITY_INVARIANTS.md`).
type oidcLogoutBridgeCookiePayload struct {
	SessionID     string `json:"session_id"`
	UserID        uint   `json:"user_id"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

// setOIDCLogoutBridgeCookie seals the bridge handle for one owner's sign-out.
// userID is required for the same reason sessionID is: a payload naming no
// owner is refused on read, so minting one would only defer the failure to a
// later request in a different place — and a zero owner must never reach the
// lookup as "any owner".
func (handler *Handler) setOIDCLogoutBridgeCookie(c fiber.Ctx, sessionID string, userID uint, now time.Time) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || userID == 0 {
		handler.clearOIDCLogoutBridgeCookie(c)
		return fiber.ErrBadRequest
	}
	if now.IsZero() {
		now = time.Now()
	}
	expiresAt := now.UTC().Add(time.Minute)
	bridgePayload := oidcLogoutBridgeCookiePayload{
		SessionID:     sessionID,
		UserID:        userID,
		ExpiresAtUnix: expiresAt.Unix(),
	}

	serialized, err := json.Marshal(bridgePayload)
	if err != nil {
		return err
	}
	return handler.writeSealedCookie(c, oidcLogoutBridgeCookieSpec, serialized, expiresAt)
}

func (handler *Handler) readOIDCLogoutBridgeCookie(c fiber.Ctx, now time.Time) oidcLogoutBridgeCookiePayload {
	raw := strings.TrimSpace(c.Cookies(oidcLogoutBridgeCookieName))
	if raw == "" {
		return oidcLogoutBridgeCookiePayload{}
	}

	decoded, err := handler.openCookieValue(oidcLogoutBridgeCookieName, raw)
	if err != nil {
		handler.clearOIDCLogoutBridgeCookie(c)
		return oidcLogoutBridgeCookiePayload{}
	}

	payload := oidcLogoutBridgeCookiePayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil || !payload.validAt(now) {
		handler.clearOIDCLogoutBridgeCookie(c)
		return oidcLogoutBridgeCookiePayload{}
	}
	return payload
}

func (handler *Handler) clearOIDCLogoutBridgeCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, oidcLogoutBridgeCookieSpec)
}

func (handler *Handler) providerLogoutRedirectURLFromState(state services.OIDCLogoutState) string {
	// The mode in force now decides, never the row: see
	// providerLogoutConfigured below.
	if !handler.providerLogoutConfigured() {
		return ""
	}
	if !validOIDCLogoutState(state, handler.oidcIssuerURL()) {
		return ""
	}
	// The return address comes from the current configuration, not from the
	// stored row: a row written under an earlier configuration would otherwise
	// tell the provider to send the browser somewhere this instance no longer
	// names. No configured address composes no provider redirect.
	postLogoutRedirectURL := handler.oidcPostLogoutRedirectURL()
	if postLogoutRedirectURL == "" {
		return ""
	}
	logoutURL, err := url.Parse(strings.TrimSpace(state.EndSessionEndpoint))
	if err != nil || !logoutURL.IsAbs() {
		return ""
	}

	query := logoutURL.Query()
	query.Set("id_token_hint", strings.TrimSpace(state.IDTokenHint))
	query.Set("post_logout_redirect_uri", postLogoutRedirectURL)
	logoutURL.RawQuery = query.Encode()
	return logoutURL.String()
}

// providerLogoutConfigured reports whether the configuration in force at this
// request asks a sign-out to travel through the provider's end-session
// endpoint — OIDC on, and a logout mode that wants the hop. It is the single
// read-time gate: the sign-out handler asks it before minting the bridge
// cookie, and providerLogoutRedirectURLFromState asks it before composing a
// Location, so the two cannot drift.
//
// A stored row is never the source of truth about the mode. Rows live for
// days (defaultOIDCLogoutStateTTL), so without this an owner who signed in
// before the operator switched OIDC_LOGOUT_MODE to local — or turned OIDC off
// — would keep being sent to the provider's end-session endpoint for the rest
// of the row's life, exercising a mode the operator had turned off. A handler
// with no OIDC service configures no provider logout.
func (handler *Handler) providerLogoutConfigured() bool {
	if handler == nil || handler.oidcService == nil {
		return false
	}
	return handler.oidcService.ProviderLogoutEnabled()
}

// oidcPostLogoutRedirectURL is the post-logout return address the current
// configuration resolves to. A handler with no OIDC service reports none.
func (handler *Handler) oidcPostLogoutRedirectURL() string {
	if handler == nil || handler.oidcService == nil {
		return ""
	}
	return strings.TrimSpace(handler.oidcService.PostLogoutRedirectURL())
}

// oidcIssuerURL is the configured issuer stored logout state is pinned to. A
// handler with no OIDC service reports none, and an absent issuer admits no
// stored end-session endpoint rather than skipping the pin.
func (handler *Handler) oidcIssuerURL() string {
	if handler == nil || handler.oidcService == nil {
		return ""
	}
	return handler.oidcService.IssuerURL()
}

func validOIDCLogoutState(payload services.OIDCLogoutState, issuerURL string) bool {
	endSessionEndpoint := strings.TrimSpace(payload.EndSessionEndpoint)
	idTokenHint := strings.TrimSpace(payload.IDTokenHint)
	postLogoutRedirectURL := strings.TrimSpace(payload.PostLogoutRedirectURL)
	if endSessionEndpoint == "" || idTokenHint == "" || postLogoutRedirectURL == "" {
		return false
	}

	// The host check and the issuer-origin pin are the ones the discovery
	// sanitizer applies, repeated here because this state is read back from a
	// row that may predate them or the issuer now configured: a stored
	// `https://:8443/logout` would otherwise be composed into a Location carrying
	// the id_token_hint to whatever listens locally on that port, and a stored
	// foreign origin would hand that hint to a host the issuer does not own.
	endpointURL, err := url.Parse(endSessionEndpoint)
	if err != nil || !endpointURL.IsAbs() || !strings.EqualFold(endpointURL.Scheme, "https") || endpointURL.Fragment != "" {
		return false
	}
	if security.HostDialsThisMachine(endpointURL.Hostname()) {
		return false
	}
	if !security.OnIssuerOrigin(endpointURL, issuerURL) {
		return false
	}

	redirectURL, err := url.Parse(postLogoutRedirectURL)
	if err != nil || !redirectURL.IsAbs() || !strings.EqualFold(redirectURL.Scheme, "https") {
		return false
	}
	if security.HostDialsThisMachine(redirectURL.Hostname()) {
		return false
	}
	if redirectURL.RawQuery != "" || redirectURL.Fragment != "" {
		return false
	}

	return true
}

func (payload oidcLogoutBridgeCookiePayload) validAt(now time.Time) bool {
	if strings.TrimSpace(payload.SessionID) == "" {
		return false
	}
	// A payload naming no owner is invalid input, never a licence to resolve
	// the session id on its own: the reader retracts it in the same response
	// rather than letting it be presented again. A bridge cookie minted by a
	// build that predates the user_id field lands here and degrades to a local
	// sign-out, bounded by this payload's own one-minute TTL above.
	if payload.UserID == 0 {
		return false
	}
	if payload.ExpiresAtUnix <= 0 {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	return now.UTC().Unix() <= payload.ExpiresAtUnix
}
