package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
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
// pending OIDC identity link.
func (handler *Handler) ShowOIDCLinkConfirmPage(c *fiber.Ctx) error {
	payload, ok := handler.readOIDCLinkPendingCookie(c)
	if !ok {
		handler.clearOIDCLinkPendingCookie(c)
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	flash := handler.popFlashCookie(c)
	messages := currentMessages(c)
	data := fiber.Map{
		"Title":      localizedPageTitle(messages, "auth.oidc.link_confirm.title", "Ovumcy | Confirm OIDC link"),
		"ErrorKey":   flash.AuthError,
		"TargetEmail": payload.Email,
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
	targetUser, err := handler.authService.FindByID(payload.TargetUserID)
	if err != nil || !targetUser.LocalAuthEnabled {
		handler.clearOIDCLinkPendingCookie(c)
		spec := authOIDCLinkConfirmUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	if _, err := handler.authService.AuthenticateCredentials(targetUser.Email, password); err != nil {
		// Do not clear the pending cookie on a single wrong password — the
		// per-IP /auth/oidc/* rate limiter (configured in main.go) bounds
		// brute-force volume. Keep the cookie so the user can retry within
		// the 5-minute TTL.
		spec := authOIDCLinkConfirmInvalidPasswordErrorSpec()
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect(oidcLinkConfirmPath, fiber.StatusSeeOther)
	}

	claims := security.OIDCClaims{
		Issuer:  payload.Issuer,
		Subject: payload.Subject,
		Email:   payload.Email,
	}
	if err := handler.oidcService.ConfirmAndLinkIdentity(payload.TargetUserID, claims, time.Now()); err != nil {
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
