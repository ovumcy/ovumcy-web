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
	// Normalize here rather than leaning on setLanguageCookie's own call: the
	// value written into the cookie is then the one this function validated,
	// and a later cookie writer that stops normalizing cannot turn a stored
	// "ru-RU" into a cookie no locale matches.
	handler.setLanguageCookie(c, handler.i18n.NormalizeLanguage(stored))
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
// consumed by their own flow, and `ovumcy_flash` carries no secret. `ovumcy_lang`
// and `ovumcy_tz` are absent for a different reason, and it is not an oversight:
// neither is sealed or session-scoped, and this helper also runs where a session
// is REJECTED rather than ended — an expired cookie on an ordinary request, or an
// unauthenticated probe. Retracting the language there would take the login
// page's language away from a visitor whose session merely lapsed, and would let
// anyone reset a browser's rendering language with one unauthenticated request.
// The deliberate ends clear both through clearSessionEndCookies below.
func (handler *Handler) clearAuthRelatedCookies(c fiber.Ctx) {
	handler.clearAuthCookie(c)
	handler.clearOIDCLogoutBridgeCookie(c)
	handler.clearRecoveryCodePageCookie(c)
	handler.clearResetPasswordCookie(c)
	handler.clearTOTPPendingCookie(c)
	handler.clearTOTPSetupCookie(c)
	handler.clearCalendarFeedRevealCookie(c)
}

// clearSessionEndCookies retracts everything clearAuthRelatedCookies does, plus
// the two plaintext preference caches `ovumcy_lang` and `ovumcy_tz`. It is the
// helper for the paths where the owner ends the session ON PURPOSE — logout and
// account deletion — as opposed to the paths where the server refuses one it was
// handed.
//
// The split is the whole point. Neither cookie is sealed or scoped to a session:
// `ovumcy_lang` is a pre-auth cache of the account column, re-issued from it by
// setAuthCookie at every session issue, and `ovumcy_tz` is the browser's own
// timezone, re-issued by LanguageMiddleware from the request header. Left behind
// by a sign-out they disclose, on a shared or borrowed browser, that this app is
// used here, in which language, and from which region — before anyone has
// authenticated. Cleared on a session REJECTION they would instead be a nuisance
// an unauthenticated caller controls, and would cost a visitor whose session
// expired mid-use the language of the page they are looking at. Deliberate end
// clears them; refusal leaves them alone.
//
// The retraction of `ovumcy_tz` holds only because the client stops volunteering
// the timezone while signed out: app.js writes the cookie and attaches the
// X-Ovumcy-Timezone header only on a page rendered for a session (see
// signedInPage in web/src/js/app/00-core.js). Without that half the next page
// load — the login page this very sign-out redirects to — would put the cookie
// straight back, and this retraction would be theatre.
func (handler *Handler) clearSessionEndCookies(c fiber.Ctx) {
	handler.clearAuthRelatedCookies(c)
	handler.clearLanguageCookie(c)
	handler.clearTimezoneCookie(c)
}

// sessionWasRemembered reads the owner's remember-me choice back off the session
// that is being replaced. The choice is already recorded — it IS the token's
// lifetime — so a re-issue carries it forward without a second source of truth,
// a new claim, or asking again on a form that has no such control. Without this
// every posture change (password, 2FA on or off, clear-data) silently demoted a
// remembered device to a session cookie, which is the same defect as remembering
// one nobody chose, pointing the other way.
//
// A token with no dates, or none on the request, is treated as not remembered:
// the default is the one an unticked box gets, so an unreadable answer costs a
// re-login at worst and never extends a session past what its owner asked for.
func sessionWasRemembered(c fiber.Ctx) bool {
	session, ok := currentAuthSession(c)
	if !ok || session == nil || session.IssuedAt == nil || session.ExpiresAt == nil {
		return false
	}
	return session.ExpiresAt.Sub(session.IssuedAt.Time) > defaultAuthTokenTTL
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

	// The rotation acts for the session being replaced, so every call below is
	// scoped to that session's owner; the loaded state carries the same owner
	// forward into the new row rather than having it re-supplied here.
	logoutState, found, err := handler.oidcLogoutStateSvc.Load(c.Context(), oldSessionID, currentSession.UserID, time.Now())
	if err != nil || !found {
		return err
	}
	if !validOIDCLogoutState(logoutState) {
		return handler.oidcLogoutStateSvc.Delete(c.Context(), oldSessionID, currentSession.UserID) // codecov:ignore -- OIDC logout-state rotation; covered by the e2e OIDC lanes
	}
	if err := handler.oidcLogoutStateSvc.Save(c.Context(), newSessionID, logoutState, time.Now()); err != nil { // codecov:ignore -- OIDC logout-state rotation; covered by the e2e OIDC lanes
		return err
	}
	return handler.oidcLogoutStateSvc.Delete(c.Context(), oldSessionID, currentSession.UserID) // codecov:ignore -- OIDC logout-state rotation; covered by the e2e OIDC lanes
}

// refreshCurrentSession re-issues the auth cookie for the request's user
// after an operation that bumped auth_session_version, so the originating
// device stays signed in while every other session is invalidated. The
// `scope` argument is used for security-event logging only.
//
// The refusal is handed BACK as a spec instead of being written here, because
// the callers do not answer on the same channel: the settings routes reply
// through respondMappedError, while a caller running on /auth/oidc/callback has
// to flash it through redirectSettingsRefusal — there the reply is a browser
// navigation coming back from the identity provider, and every spec this can
// raise renders either a JSON envelope (the global session-create spec) or a
// redirect to /login (the unsupported-role spec) instead of the settings page
// the step-up promised to return to.
//
// That is also why the wrapper this replaced is gone rather than kept for the
// callers whose channel it did suit. It answered through respondMappedError and
// returned respondMappedError's nil, so it returned nil on BOTH paths: every
// `if err := ...; err != nil` around it was dead, and each caller ran on into
// its success arm, writing a success toast, a redirect or a `data_cleared`
// flash over the refusal that had just been written.
func (handler *Handler) refreshCurrentSession(c fiber.Ctx, user *models.User, scope string) (APIErrorSpec, bool) {
	sessionID, err := handler.setAuthCookie(c, user, sessionWasRemembered(c))
	if err != nil {
		handler.clearAuthCookie(c)
		spec := authSessionCreateErrorSpec()
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec = authWebSignInUnavailableErrorSpec()
		}
		handler.logSecurityError(c, scope, spec)
		return spec, false
	}
	if err := handler.rotateOIDCLogoutState(c, sessionID); err != nil {
		handler.logSecurityEvent(c, scope, "provider_logout_state_rotation_failed")
	}
	return APIErrorSpec{}, true
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
