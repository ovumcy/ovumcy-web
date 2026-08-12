package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

var authCookieSpec = sealedCookieSpec{name: authCookieName, path: "/"}

func (handler *Handler) setAuthCookie(c fiber.Ctx, user *models.User, rememberMe bool) (string, error) {
	tokenTTL := defaultAuthTokenTTL
	if rememberMe {
		tokenTTL = rememberAuthTokenTTL
	}

	token, sessionID, err := handler.buildTokenWithSessionID(user, tokenTTL)
	if err != nil {
		return "", err
	}
	// Session-scoped unless remember-me: a zero expires keeps the cookie
	// for the browser session while the token payload carries its own TTL.
	var expires time.Time
	if rememberMe {
		expires = time.Now().Add(tokenTTL)
	}
	if err := handler.writeSealedCookie(c, authCookieSpec, []byte(token), expires); err != nil {
		return "", err
	}
	handler.applyStoredLanguage(c, user)
	return sessionID, nil
}

// applyStoredLanguage re-issues the language cookie from the account's stored
// interface language, so a device that has no cookie — a fresh browser, a
// cleared cookie jar, a second machine — is served the language its owner chose
// instead of falling back to Accept-Language.
//
// It sits inside setAuthCookie on purpose. Every session-issue path goes
// through that one helper (password login, TOTP challenge completion, OIDC
// callback, OIDC link-confirm, register pickup, recovery sign-in, and the
// in-place re-issue after a security-posture change), so the preference cannot
// hold on one of them and silently not on the next one added.
//
// An empty column means the owner never chose a language: nothing is written,
// and resolveRequestLanguage keeps deciding exactly as it did before the column
// existed. An unsupported stored code is treated the same way rather than
// normalized to the default — NormalizeLanguage would happily answer with the
// operator default for it, which would pin an account to a language nobody
// picked. Writing the cookie only for a value the shipped catalogue actually
// carries keeps an unknown code a no-op instead of a 500 or a wrong locale.
//
// The caller has already refused a nil user: buildTokenWithSessionID runs
// first and returns an error for one, so no session exists to attach a language
// to. There is deliberately no second nil check here for a state that cannot
// reach this line.
func (handler *Handler) applyStoredLanguage(c fiber.Ctx, user *models.User) {
	stored := strings.TrimSpace(user.InterfaceLanguage)
	if stored == "" || !handler.i18n.IsSupportedLanguage(stored) {
		return
	}
	handler.setLanguageCookie(c, stored)
}

func (handler *Handler) clearAuthCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, authCookieSpec)
}

// clearAuthRelatedCookies retracts every sealed cookie whose contents belong to
// the session being ended: the session itself, the provider-logout bridge, and
// each in-flight handoff carrying account-scoped state or a secret — recovery
// code, reset token, pending 2FA challenge, the pending TOTP enrollment secret,
// and the one-time calendar-feed subscribe URL.
//
// None of those is consumable without an authenticated session, so anything left
// behind is exposure with no remaining purpose. Both TOTP cookies are
// session-scoped (zero expires), so an abandoned enrollment would otherwise keep
// shipping its sealed raw secret at `path: "/"` for the rest of the browser
// session, and the feed URL stays a live capability the sign-out does not revoke.
// Retracting them here is what bounds that window, rather than waiting for some
// later request to happen to read and refuse the value.
//
// Cookies whose flow completes outside a session are deliberately absent: the
// OIDC one-time state and step-up cookies and the register-pickup handle are
// consumed by their own flow, and `ovumcy_flash` carries no secret.
func (handler *Handler) clearAuthRelatedCookies(c fiber.Ctx) {
	handler.clearAuthCookie(c)
	handler.clearOIDCLogoutBridgeCookie(c)
	handler.clearRecoveryCodePageCookie(c)
	handler.clearResetPasswordCookie(c)
	handler.clearTOTPPendingCookie(c)
	handler.clearTOTPSetupCookie(c)
	handler.clearCalendarFeedRevealCookie(c)
}

func (handler *Handler) buildTokenWithSessionID(user *models.User, ttl time.Duration) (string, string, error) {
	if user == nil {
		return "", "", errors.New("user is required")
	}
	if err := services.ValidateSupportedWebUser(user); err != nil {
		return "", "", err
	}
	if ttl <= 0 {
		ttl = defaultAuthTokenTTL
	}
	return handler.authService.BuildAuthSessionTokenWithSessionID(handler.secretKey, user.ID, user.Role, user.AuthSessionVersion, ttl, time.Now())
}

func (handler *Handler) rotateOIDCLogoutState(c fiber.Ctx, newSessionID string) error {
	if handler == nil || handler.oidcLogoutStateSvc == nil {
		return nil
	}

	newSessionID = strings.TrimSpace(newSessionID)
	if newSessionID == "" {
		return nil
	}

	currentSession, ok := currentAuthSession(c)
	if !ok || currentSession == nil {
		return nil
	}

	oldSessionID := strings.TrimSpace(currentSession.SessionID)
	if oldSessionID == "" || oldSessionID == newSessionID {
		return nil
	}

	logoutState, found, err := handler.oidcLogoutStateSvc.Load(c.Context(), oldSessionID, time.Now())
	if err != nil || !found {
		return err
	}
	if !validOIDCLogoutState(logoutState) {
		return handler.oidcLogoutStateSvc.Delete(c.Context(), oldSessionID) // codecov:ignore -- OIDC logout-state rotation; covered by the e2e OIDC lanes
	}
	logoutState.UserID = currentSession.UserID
	if err := handler.oidcLogoutStateSvc.Save(c.Context(), newSessionID, logoutState, time.Now()); err != nil { // codecov:ignore -- OIDC logout-state rotation; covered by the e2e OIDC lanes
		return err
	}
	return handler.oidcLogoutStateSvc.Delete(c.Context(), oldSessionID) // codecov:ignore -- OIDC logout-state rotation; covered by the e2e OIDC lanes
}

// refreshCurrentSession re-issues the auth cookie for the request's user
// after an operation that bumped auth_session_version, so the originating
// device stays signed in while every other session is invalidated. The
// `scope` argument is used for security-event logging only.
func (handler *Handler) refreshCurrentSession(c fiber.Ctx, user *models.User, scope string) error {
	sessionID, err := handler.setAuthCookie(c, user, false)
	if err != nil {
		handler.clearAuthCookie(c)
		spec := authSessionCreateErrorSpec()
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec = authWebSignInUnavailableErrorSpec()
		}
		handler.logSecurityError(c, scope, spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.rotateOIDCLogoutState(c, sessionID); err != nil {
		handler.logSecurityEvent(c, scope, "provider_logout_state_rotation_failed")
	}
	return nil
}

func (handler *Handler) encodeAuthCookieToken(rawToken string) (string, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return "", errors.New("auth token is required")
	}

	codec, err := handler.cookieCodec()
	if err != nil {
		return "", err
	}
	return codec.seal(authCookieName, []byte(rawToken))
}

func (handler *Handler) decodeSealedAuthCookieToken(rawValue string) (string, error) {
	codec, err := handler.cookieCodec()
	if err != nil {
		return "", err
	}

	plaintext, err := codec.open(authCookieName, rawValue)
	if err != nil {
		return "", err
	}

	token := strings.TrimSpace(string(plaintext))
	if token == "" {
		return "", errors.New("auth token is required")
	}
	return token, nil
}
