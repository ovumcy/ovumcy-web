package api

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

type oidcLogoutBridgeCookiePayload struct {
	SessionID     string `json:"session_id"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

func (handler *Handler) setOIDCLogoutBridgeCookie(c *fiber.Ctx, sessionID string, now time.Time) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		handler.clearOIDCLogoutBridgeCookie(c)
		return fiber.ErrBadRequest
	}
	if now.IsZero() {
		now = time.Now()
	}
	expiresAt := now.UTC().Add(time.Minute)
	bridgePayload := oidcLogoutBridgeCookiePayload{
		SessionID:     sessionID,
		ExpiresAtUnix: expiresAt.Unix(),
	}

	serialized, err := json.Marshal(bridgePayload)
	if err != nil {
		return err
	}
	encoded, err := handler.sealCookieValue(oidcLogoutBridgeCookieName, serialized)
	if err != nil {
		return err
	}

	c.Cookie(&fiber.Cookie{
		Name:     oidcLogoutBridgeCookieName,
		Value:    encoded,
		Path:     oidcLogoutBridgePath,
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  expiresAt,
	})
	return nil
}

func (handler *Handler) readOIDCLogoutBridgeCookie(c *fiber.Ctx, now time.Time) oidcLogoutBridgeCookiePayload {
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

func (handler *Handler) clearOIDCLogoutBridgeCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     oidcLogoutBridgeCookieName,
		Value:    "",
		Path:     oidcLogoutBridgePath,
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}

func (handler *Handler) clearOIDCLogoutTransportCookies(c *fiber.Ctx) {
	handler.clearOIDCLogoutBridgeCookie(c)
}

func (handler *Handler) providerLogoutRedirectURLFromState(state services.OIDCLogoutState) string {
	if !validOIDCLogoutState(state) {
		return ""
	}
	logoutURL, err := url.Parse(strings.TrimSpace(state.EndSessionEndpoint))
	if err != nil || !logoutURL.IsAbs() {
		return ""
	}

	query := logoutURL.Query()
	query.Set("id_token_hint", strings.TrimSpace(state.IDTokenHint))
	query.Set("post_logout_redirect_uri", strings.TrimSpace(state.PostLogoutRedirectURL))
	logoutURL.RawQuery = query.Encode()
	return logoutURL.String()
}

func (handler *Handler) sealCookieValue(cookieName string, plaintext []byte) (string, error) {
	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return "", err
	}
	return codec.seal(cookieName, plaintext)
}

func (handler *Handler) openCookieValue(cookieName string, raw string) ([]byte, error) {
	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return nil, err
	}
	return codec.open(cookieName, raw)
}

func validOIDCLogoutState(payload services.OIDCLogoutState) bool {
	endSessionEndpoint := strings.TrimSpace(payload.EndSessionEndpoint)
	idTokenHint := strings.TrimSpace(payload.IDTokenHint)
	postLogoutRedirectURL := strings.TrimSpace(payload.PostLogoutRedirectURL)
	if endSessionEndpoint == "" || idTokenHint == "" || postLogoutRedirectURL == "" {
		return false
	}

	endpointURL, err := url.Parse(endSessionEndpoint)
	if err != nil || !endpointURL.IsAbs() || !strings.EqualFold(endpointURL.Scheme, "https") || endpointURL.Fragment != "" {
		return false
	}

	redirectURL, err := url.Parse(postLogoutRedirectURL)
	if err != nil || !redirectURL.IsAbs() || !strings.EqualFold(redirectURL.Scheme, "https") {
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
	if payload.ExpiresAtUnix <= 0 {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	return now.UTC().Unix() <= payload.ExpiresAtUnix
}
