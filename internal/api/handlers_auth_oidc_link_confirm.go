package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// startOIDCLinkConfirmation is dispatched from CompleteOIDCLogin when the
// service layer returned ErrOIDCLinkRequiresConfirmation — the OIDC exchange
// resolved to a pre-existing local user by email but the (issuer, subject)
// pair has never been linked. Auto-linking in that situation would let a
// malicious or sloppy upstream IdP take over the account.
//
// This used to seal a pending-link cookie and hand the user off to a
// password-confirmation page reachable without a session
// (/auth/oidc/link-confirm below). Issue #701 closes that route for good:
// linking is a permanent, password-change-weight binding and must be
// authorised by a factor verified NOW, which an unauthenticated page cannot
// do. There are now exactly two ways to complete this link — sign in with the
// existing method and use the Settings step-up
// (StartOIDCIdentityLinkStepup/completeOIDCIdentityLinkStepup, both reusing
// this same OIDCLoginService.ConfirmAndLinkIdentity), or have the operator run
// `ovumcy link-oidc-identity` (internal/cli) for an account with no session at
// all. This function therefore never mints the pending-link cookie anymore —
// ShowOIDCLinkConfirmPage and CompleteOIDCLinkConfirmation below are
// unreachable with a real cookie as a result, which is what keeps the public
// route closed without requiring every caller of this handler to re-derive
// that decision.
func (handler *Handler) startOIDCLinkConfirmation(c fiber.Ctx, result services.OIDCLoginResult) error {
	spec := authOIDCLinkConfirmUnavailableErrorSpec()
	handler.logSecurityError(c, "auth.oidc_callback", spec)
	handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
	return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
}

// ShowOIDCLinkConfirmPage renders the password challenge that gates the
// pending OIDC identity link. When the target account has TOTP enabled, the
// page also surfaces the 2FA code field so the link cannot be completed
// without the second factor.
func (handler *Handler) ShowOIDCLinkConfirmPage(c fiber.Ctx) error {
	payload, ok := handler.readOIDCLinkPendingCookie(c)
	if !ok {
		handler.clearOIDCLinkPendingCookie(c)
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	totpRequired := false
	if targetUser, err := handler.authService.FindByID(c.Context(), payload.TargetUserID); err == nil {
		// Verifiable, not the raw TOTPEnabled column: an account enrolled in
		// TOTP whose secret does not decrypt (SECRET_KEY rotation) cannot
		// answer a code challenge at all, so the page must not ask for one —
		// CompleteOIDCLinkConfirmation below routes that account through
		// LoginService's forced-reset result instead.
		totpRequired = handler.totpService.Verifiable(targetUser)
	}

	flash := handler.popFlashCookie(c)
	messages := currentMessages(c)
	data := fiber.Map{
		"Title": localizedPageTitle(messages, "auth.oidc.link_confirm.title", "Ovumcy | Confirm OIDC link"),
		// Same resolution the other auth pages perform: the flash holds the error
		// spec key, the template translates ErrorKey, so the spec key has to become
		// a locale key here or the page renders it verbatim.
		"ErrorKey":     services.AuthErrorTranslationKey(services.ResolveAuthErrorSource(flash.AuthError)),
		"TargetEmail":  payload.Email,
		"TOTPRequired": totpRequired,
	}
	return handler.render(c, "auth_oidc_link_confirm", data)
}

// CompleteOIDCLinkConfirmation verifies the current password for the target
// account and, on success, persists the OIDC identity link and issues a fresh
// auth session for the target user.
//
// As of issue #701, startOIDCLinkConfirmation never mints the sealed
// pending-link cookie this function reads, on any account, in any
// configuration — see that function's comment. Nothing in the OIDC callback
// path calls into this function anymore; it stays only because it is still
// unit-tested directly (a hand-sealed cookie built the same way the old mint
// used to), documenting the gate below rather than guarding a live route. The
// two ways to actually create a link today are the Settings step-up
// (completeOIDCIdentityLinkStepup) and the operator CLI's
// `ovumcy link-oidc-identity`, both calling OIDCLoginService.ConfirmAndLinkIdentity
// directly. The reasoning that follows is kept for the record and because the
// same gate would matter again if this function were ever reconnected.
func (handler *Handler) CompleteOIDCLinkConfirmation(c fiber.Ctx) error {
	// Same instance-level gate, same point in the handler, as Login
	// (handlers_auth_session_login.go): checked first, before anything else
	// runs. The password verified below is exactly the factor the operator
	// switched off, and it authorizes more than the session this handler used
	// to mint directly — it authorizes ConfirmAndLinkIdentity, a PERMANENT
	// binding of an attacker-supplied (issuer, subject) to the target account.
	// Gating only the session mint and leaving the link itself unguarded does
	// not close that: an attacker who holds the leaked password the operator
	// disabled local sign-in over — the realistic reason to disable it — can
	// still drive the OIDC callback with an IdP identity carrying the victim's
	// email, hit ErrOIDCLinkRequiresConfirmation, post that password here to
	// commit the link, and then sign in through the identity just linked on
	// the very next OIDC round-trip — no password prompt at all the second
	// time, because authenticateLinkedIdentity never asks. A gate placed after
	// the link is one hop deep, not a gate.
	if !handler.localPublicAuthEnabled() {
		spec := authLocalSignInDisabledErrorSpec()
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	payload, ok := handler.readOIDCLinkPendingCookie(c)
	if !ok {
		handler.clearOIDCLinkPendingCookie(c)
		spec := authOIDCLinkConfirmExpiredErrorSpec()
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	password := strings.TrimSpace(c.FormValue("password"))
	if password == "" {
		spec := authOIDCLinkConfirmInvalidPasswordErrorSpec()
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To(oidcLinkConfirmPath)
	}

	// Cross-check that the cookie still resolves to a live user with local
	// auth enabled. If the user's local auth was disabled between cookie
	// issuance and submission, refuse — confirming via password no longer
	// proves possession.
	targetUser, err := handler.authService.FindByID(c.Context(), payload.TargetUserID)
	if err != nil || !targetUser.LocalAuthEnabled {
		handler.clearOIDCLinkPendingCookie(c)
		spec := authOIDCLinkConfirmUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	// Verify the password through the same LoginService path as the login
	// form, so link-confirm shares the per-(client, identity) failure budget
	// with login. The per-IP /auth/oidc/* HTTP limiter only bounds raw
	// request volume; without the shared attempt policy this endpoint was a
	// faster password oracle than the login form it mirrors.
	result, err := handler.loginService.Authenticate(
		c.Context(),
		handler.secretKey,
		c.IP(),
		targetUser.Email,
		password,
		30*time.Minute,
		time.Now(),
	)
	if err != nil {
		// Do not clear the pending cookie on a failed attempt — keep it so
		// the user can retry within the 5-minute TTL.
		spec := mapOIDCLinkConfirmPasswordError(err)
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To(oidcLinkConfirmPath)
	}

	// Step-up 2FA gate. If the target user's TOTP is verifiable, the
	// link-confirm submission must also carry a valid TOTP code in the same
	// form — mirroring the local-password Login flow that redirects
	// TOTP-enabled accounts to /auth/2fa before issuing a session. Without
	// this gate, an attacker with the victim's password plus a
	// malicious/sloppy upstream IdP (the threat link-confirm was added to
	// mitigate) could obtain the session issued a few lines down for a
	// TOTP-protected account without ever holding the second factor. A
	// subsequent OIDC sign-in through the now-linked identity no longer
	// compounds that: CompleteOIDCLogin gates a linked identity's
	// re-authentication on the same second factor (OIDCLoginResult.RequiresTOTP),
	// so this gate's job is only the session minted right here, not any future
	// one. Keep the link pending cookie alive on TOTP failure so the user can
	// retry within TTL, same as wrong-password.
	//
	// Gate on Verifiable, not the raw TOTPEnabled column: an enrolled but
	// unverifiable secret (SECRET_KEY rotation) can never satisfy this
	// challenge, so demanding one here would strand the user on a
	// perpetually-failing form. `result`, computed above by
	// loginService.Authenticate, already carries RequiresPasswordReset=true
	// for that account (the same forced-reset reasoning LoginService applies
	// to the local login route), so skipping the challenge here does not
	// bypass the factor — it falls through to the reset branch below.
	if handler.totpService.Verifiable(targetUser) {
		if spec, ok := handler.verifyTOTPForLinkConfirm(c, &targetUser); !ok {
			handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To(oidcLinkConfirmPath)
		}
	}

	claims := security.OIDCClaims{
		Issuer:  payload.Issuer,
		Subject: payload.Subject,
		Email:   payload.Email,
	}
	if err := handler.oidcService.ConfirmAndLinkIdentity(c.Context(), payload.TargetUserID, claims, time.Now()); err != nil {
		handler.clearOIDCLinkPendingCookie(c)
		spec := mapOIDCLinkConfirmError(err)
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	handler.clearOIDCLinkPendingCookie(c)

	if result.RequiresPasswordReset {
		// result comes from loginService.Authenticate above (:137-145), which
		// verified a LOCAL password for targetUser — this handler never checks
		// an instance-wide OIDC gate before minting. LoginService therefore
		// always issues PasswordResetTokenPurposeForcedLocal here, never
		// forced-from-OIDC: labelling this mint OIDC would let the redeem gate
		// bypass the local-sign-in toggle on exactly the path that just proved
		// a local password. ConfirmAndLinkIdentity already committed the link
		// above (the call a few lines up in this same function), so a
		// subsequent plain OIDC sign-in for this user still reaches
		// CompleteOIDCLogin and mints a genuine forced-from-OIDC token if one
		// is still needed — nothing is stranded by the local label.
		if err := handler.setResetPasswordCookie(c, result.ResetToken); err != nil {
			// codecov:ignore:start -- defensive: sealing the reset cookie fails only on cipher init errors, which a boot-validated SECRET_KEY cannot produce in-process.
			spec := authResetTokenCreateErrorSpec()
			handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
			// codecov:ignore:end
		}
		handler.logSecurityEvent(c, "auth.oidc_link_confirm", "reset_required")
		return c.Redirect().Status(fiber.StatusSeeOther).To("/reset-password")
	}

	if _, err := handler.setAuthCookie(c, &targetUser, false); err != nil {
		// codecov:ignore:start -- defensive: the LoginService password gate above already refuses
		// unsupported roles (TestFullPageFallbackLinkConfirmRejectsUnsupportedRoleTarget), so this
		// arm is reachable only through an AEAD seal error.
		spec := authSessionCreateErrorSpec()
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec = authOIDCAccountUnavailableErrorSpec()
		}
		handler.logSecurityError(c, "auth.oidc_link_confirm", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		// codecov:ignore:end
	}

	handler.logSecurityEvent(c, "auth.oidc_link_confirm", "linked")
	return c.Redirect().Status(fiber.StatusSeeOther).To(services.PostLoginRedirectPath(&targetUser))
}

// verifyTOTPForLinkConfirm runs the same checks as VerifyTOTPLogin (rate-limit,
// length, replay, validity) and returns ok=true only when the code passes.
// On failure it returns an APIErrorSpec the caller can flash + redirect with.
// On success it resets the per-(ip,user) failure counter so a clean unlink+
// relink cycle doesn't carry stale attempts.
func (handler *Handler) verifyTOTPForLinkConfirm(c fiber.Ctx, targetUser *models.User) (APIErrorSpec, bool) {
	if err := handler.totpService.CheckRateLimit(handler.secretKey, c.IP(), targetUser.ID, time.Now()); err != nil {
		return totpRateLimitedErrorSpec(), false
	}
	code := strings.TrimSpace(c.FormValue("totp_code"))
	if len(code) != 6 {
		handler.totpService.RecordFailure(handler.secretKey, c.IP(), targetUser.ID, time.Now())
		return totpInvalidCodeErrorSpec(), false
	}
	valid, err := handler.totpService.ValidateCode(c.Context(), targetUser.ID, targetUser.TOTPSecret, code)
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
