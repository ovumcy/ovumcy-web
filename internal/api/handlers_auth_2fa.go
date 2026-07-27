package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// ShowTOTPChallengePage renders the 2FA code entry page after a successful
// password login when the user has TOTP enabled.
func (handler *Handler) ShowTOTPChallengePage(c fiber.Ctx) error {
	_, _, err := handler.parseTOTPPendingCookie(c)
	if err != nil {
		// No valid pending cookie — send back to login.
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	flash := handler.popFlashCookie(c)
	messages := currentMessages(c)
	data := fiber.Map{
		"Title": localizedPageTitle(messages, "auth.2fa.title", "Ovumcy | Two-Factor Authentication"),
		// The flash carries the error SPEC key ("totp invalid code"); the template
		// translates whatever ErrorKey holds, so it has to be resolved to a locale
		// key here — exactly as the other auth pages do. Passing the spec key
		// straight through rendered it verbatim in every language.
		"ErrorKey": services.AuthErrorTranslationKey(services.ResolveAuthErrorSource(flash.AuthError)),
	}
	return handler.render(c, "auth_2fa", data)
}

// parseTOTPChallengeCode reads the submitted code from either transport the
// endpoint serves: the JSON body `docs/openapi.yaml` publishes for API clients,
// and the urlencoded form the challenge page posts. `c.FormValue` alone never
// sees a JSON body, so a spec-conforming client got `totp invalid code` for
// every code it sent — indistinguishable from a wrong one.
func parseTOTPChallengeCode(c fiber.Ctx) string {
	if hasJSONBody(c) {
		input := totpChallengeInput{}
		if err := c.Bind().Body(&input); err != nil {
			return ""
		}
		return strings.TrimSpace(input.Code)
	}
	return strings.TrimSpace(c.FormValue("code"))
}

// VerifyTOTPLogin validates the 6-digit TOTP code submitted on the challenge page.
// On success it issues the auth session cookie and redirects to the dashboard.
func (handler *Handler) VerifyTOTPLogin(c fiber.Ctx) error {
	userID, rememberMe, err := handler.parseTOTPPendingCookie(c)
	if err != nil {
		spec := totpSessionExpiredErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	if err := handler.totpService.CheckRateLimit(handler.secretKey, c.IP(), userID, time.Now()); err != nil {
		// Invalidate the pending session so an exhausted (or stolen) cookie
		// cannot be reused; the user must re-authenticate with their password
		// to obtain a fresh challenge.
		handler.clearTOTPPendingCookie(c)
		spec := totpRateLimitedErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	code := parseTOTPChallengeCode(c)
	if len(code) != 6 {
		spec := totpInvalidCodeErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	user, err := handler.authService.FindByID(c.Context(), userID)
	if err != nil || !user.TOTPEnabled {
		spec := totpSessionExpiredErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	valid, err := handler.totpService.ValidateCode(c.Context(), userID, user.TOTPSecret, code)
	if errors.Is(err, services.ErrTOTPReplayed) {
		// Same response shape as a plain invalid code so an attacker cannot
		// distinguish replay from a wrong guess. We log replay separately for
		// security observability (potential captured-code attempt).
		handler.totpService.RecordFailure(handler.secretKey, c.IP(), userID, time.Now())
		spec := totpInvalidCodeErrorSpec()
		handler.logSecurityEvent(c, "auth.2fa", "replay_rejected")
		return handler.respondMappedError(c, spec)
	}
	if err != nil {
		spec := totpInternalErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}
	if !valid {
		handler.totpService.RecordFailure(handler.secretKey, c.IP(), userID, time.Now())
		spec := totpInvalidCodeErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.totpService.ResetAttempts(handler.secretKey, c.IP(), userID)
	handler.clearTOTPPendingCookie(c)

	if _, err := handler.setAuthCookie(c, &user, rememberMe); err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, "auth.2fa", "success")
	return redirectOrJSON(c, "/")
}
