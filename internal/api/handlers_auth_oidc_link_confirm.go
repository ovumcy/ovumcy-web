package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// startOIDCLinkConfirmation is dispatched from CompleteOIDCLogin when the
// service layer returned ErrOIDCLinkRequiresConfirmation — the OIDC exchange
// resolved to a pre-existing local user by email but the (issuer, subject)
// pair has never been linked. Auto-linking in that situation would let a
// malicious or sloppy upstream IdP take over the account, so we issue a sealed
// pending-link cookie and hand the user off to the password-confirmation page.
func (handler *Handler) startOIDCLinkConfirmation(c *fiber.Ctx, result services.OIDCLoginResult) error {
	if result.PendingLinkClaims == nil || result.User.ID == 0 {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	if !result.User.LocalAuthEnabled {
		// The target account has no local password (OIDC-only). The
		// password-confirmation step cannot succeed, so refuse rather than
		// strand the user on a perpetually-failing form. Adding a second OIDC
		// provider to such an account is a deliberate Settings action and is
		// out of scope for the unauthenticated login path.
		spec := authOIDCLinkConfirmUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	payload, err := newOIDCLinkPendingPayload(
		time.Now(),
		result.User.ID,
		result.PendingLinkClaims.Issuer,
		result.PendingLinkClaims.Subject,
		result.User.Email,
	)
	if err != nil {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	if err := handler.setOIDCLinkPendingCookie(c, payload); err != nil {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	handler.logSecurityEvent(c, "auth.oidc_callback", "link_confirmation_required")
	return c.Redirect(oidcLinkConfirmPath, fiber.StatusSeeOther)
}

// ShowOIDCLinkConfirmPage renders the password challenge that gates the
// pending OIDC identity link. When the target account has TOTP enabled, the
// page also surfaces the 2FA code field so the link cannot be completed
// without the second factor.
func (handler *Handler) ShowOIDCLinkConfirmPage(c *fiber.Ctx) error {
	payload, ok := handler.readOIDCLinkPendingCookie(c)
	if !ok {
		handler.clearOIDCLinkPendingCookie(c)
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	totpRequired := false
	if targetUser, err := handler.authService.FindByID(c.UserContext(), payload.TargetUserID); err == nil {
		totpRequired = targetUser.TOTPEnabled
	}

	flash := handler.popFlashCookie(c)
	messages := currentMessages(c)
	data := fiber.Map{
		"Title":        localizedPageTitle(messages, "auth.oidc.link_confirm.title", "Ovumcy | Confirm OIDC link"),
		"ErrorKey":     flash.AuthError,
		"TargetEmail":  payload.Email,
		"TOTPRequired": totpRequired,
	}
	return handler.render(c, "auth_oidc_link_confirm", data)
}

// CompleteOIDCLinkConfirmation verifies the current password for the target
// account and, on success, persists the OIDC identity link and issues a fresh
// auth session for the target user.
func (handler *Handler) CompleteOIDCLinkConfirmation(c *fiber.Ctx) error {
	payload, ok := handler.readOIDCLinkPendingCookie(c)
	if !ok {
		handler.clearOIDCLinkPendingCookie(c)
		spec := authOIDCLinkConfirmExpiredErrorSpec()
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	password := strings.TrimSpace(c.FormValue("password"))
	if password == "" {
		spec := authOIDCLinkConfirmInvalidPasswordErrorSpec()
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect(oidcLinkConfirmPath, fiber.StatusSeeOther)
	}

	// Cross-check that the cookie still resolves to a live user with local
	// auth enabled. If the user's local auth was disabled between cookie
	// issuance and submission, refuse — confirming via password no longer
	// proves possession.
	targetUser, err := handler.authService.FindByID(c.UserContext(), payload.TargetUserID)
	if err != nil || !targetUser.LocalAuthEnabled {
		handler.clearOIDCLinkPendingCookie(c)
		spec := authOIDCLinkConfirmUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	if _, err := handler.authService.AuthenticateCredentials(c.UserContext(), targetUser.Email, password); err != nil {
		// Do not clear the pending cookie on a single wrong password — the
		// per-IP /auth/oidc/* rate limiter (configured in main.go) bounds
		// brute-force volume. Keep the cookie so the user can retry within
		// the 5-minute TTL.
		spec := authOIDCLinkConfirmInvalidPasswordErrorSpec()
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect(oidcLinkConfirmPath, fiber.StatusSeeOther)
	}

	// Step-up 2FA gate. If the target user has TOTP enabled, the link-confirm
	// submission must also carry a valid TOTP code in the same form — mirroring
	// the local-password Login flow that redirects TOTP-enabled accounts to
	// /auth/2fa before issuing a session. Without this gate, an attacker with
	// the victim's password plus a malicious/sloppy upstream IdP (the threat
	// link-confirm was added to mitigate) could obtain a session for a
	// TOTP-protected account without ever holding the second factor — and the
	// linked identity would persist for future OIDC sign-ins. Keep the link
	// pending cookie alive on TOTP failure so the user can retry within TTL,
	// same as wrong-password.
	if targetUser.TOTPEnabled {
		if spec, ok := handler.verifyTOTPForLinkConfirm(c, &targetUser); !ok {
			handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect(oidcLinkConfirmPath, fiber.StatusSeeOther)
		}
	}

	claims := security.OIDCClaims{
		Issuer:  payload.Issuer,
		Subject: payload.Subject,
		Email:   payload.Email,
	}
	if err := handler.oidcService.ConfirmAndLinkIdentity(c.UserContext(), payload.TargetUserID, claims, time.Now()); err != nil {
		handler.clearOIDCLinkPendingCookie(c)
		spec := mapOIDCLinkConfirmError(err)
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	handler.clearOIDCLinkPendingCookie(c)

	if targetUser.MustChangePassword {
		token, issueErr := handler.passwordResetSvc.IssueResetTokenForUser(handler.secretKey, &targetUser, 30*time.Minute, time.Now())
		if issueErr != nil {
			spec := authResetTokenCreateErrorSpec()
			handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect("/login", fiber.StatusSeeOther)
		}
		if err := handler.setResetPasswordCookie(c, token, true); err != nil {
			spec := authResetTokenCreateErrorSpec()
			handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect("/login", fiber.StatusSeeOther)
		}
		handler.logSecurityEvent(c, "auth.oidc_link_confirm", "reset_required")
		return c.Redirect("/reset-password", fiber.StatusSeeOther)
	}

	if _, err := handler.setAuthCookie(c, &targetUser, false); err != nil {
		spec := authSessionCreateErrorSpec()
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec = authOIDCAccountUnavailableErrorSpec()
		}
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	handler.logSecurityEvent(c, "auth.oidc_link_confirm", "linked")
	return c.Redirect(services.PostLoginRedirectPath(&targetUser), fiber.StatusSeeOther)
}

// verifyTOTPForLinkConfirm runs the same checks as VerifyTOTPLogin (rate-limit,
// length, replay, validity) and returns ok=true only when the code passes.
// On failure it returns an APIErrorSpec the caller can flash + redirect with.
// On success it resets the per-(ip,user) failure counter so a clean unlink+
// relink cycle doesn't carry stale attempts.
func (handler *Handler) verifyTOTPForLinkConfirm(c *fiber.Ctx, targetUser *models.User) (APIErrorSpec, bool) {
	if err := handler.totpService.CheckRateLimit(handler.secretKey, c.IP(), targetUser.ID, time.Now()); err != nil {
		return totpRateLimitedErrorSpec(), false
	}
	code := strings.TrimSpace(c.FormValue("totp_code"))
	if len(code) != 6 {
		handler.totpService.RecordFailure(handler.secretKey, c.IP(), targetUser.ID, time.Now())
		return totpInvalidCodeErrorSpec(), false
	}
	valid, err := handler.totpService.ValidateCode(c.UserContext(), targetUser.ID, targetUser.TOTPSecret, code)
	if errors.Is(err, services.ErrTOTPReplayed) {
		handler.totpService.RecordFailure(handler.secretKey, c.IP(), targetUser.ID, time.Now())
		handler.logSecurityEvent(c, "auth.oidc_link_confirm", "totp_replay_rejected")
		return totpInvalidCodeErrorSpec(), false
	}
	if err != nil {
		return totpInternalErrorSpec(), false
	}
	if !valid {
		handler.totpService.RecordFailure(handler.secretKey, c.IP(), targetUser.ID, time.Now())
		return totpInvalidCodeErrorSpec(), false
	}
	handler.totpService.ResetAttempts(handler.secretKey, c.IP(), targetUser.ID)
	return APIErrorSpec{}, true
}

func mapOIDCLinkConfirmError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrOIDCLinkFailed),
		errors.Is(err, services.ErrOIDCIdentityResolveFailed):
		return authOIDCUnavailableErrorSpec()
	case errors.Is(err, services.ErrOIDCDisabled),
		errors.Is(err, services.ErrOIDCUnavailable):
		return authOIDCUnavailableErrorSpec()
	default:
		return authOIDCAuthenticationFailedErrorSpec()
	}
}
