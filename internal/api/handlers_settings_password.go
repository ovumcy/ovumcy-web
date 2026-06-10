package api

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// stepupReauthMaxAge is the maximum acceptable age of an OIDC ID token's
// auth_time / iat claim relative to the moment we finish the step-up callback.
// 5 minutes is long enough for an interactive sign-in (typically 30-60 seconds)
// but short enough that a captured ID token from an earlier session cannot be
// replayed to bypass the re-auth requirement.
const stepupReauthMaxAge = 5 * time.Minute

func (handler *Handler) ChangePassword(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "auth.password_change", spec)
		return handler.respondMappedError(c, spec)
	}

	input, err := parseChangePasswordInput(c)
	if err != nil {
		spec := settingsInvalidInputErrorSpec()
		handler.logSecurityError(c, "auth.password_change", spec)
		return handler.respondMappedError(c, spec)
	}

	if !user.LocalAuthEnabled {
		// CVE-class issue #3: previously this branch silently enabled a fresh
		// local password without any re-authentication, so a hijacked
		// OIDC-only session became permanent. The dedicated step-up flow at
		// StartLocalPasswordSetupReauth performs a fresh OIDC sign-in before
		// committing the new password.
		spec := settingsOIDCReauthRequiredErrorSpec()
		handler.logSecurityError(c, "auth.password_change", spec)
		return handler.respondMappedError(c, spec)
	}

	if err := handler.settingsService.ChangePassword(c.UserContext(), user, input.CurrentPassword, input.NewPassword, input.ConfirmPassword); err != nil {
		return handler.respondPasswordChangeError(c, err)
	}

	if err := handler.refreshPasswordChangeSession(c, user); err != nil {
		return err
	}

	handler.logSecurityEvent(c, "auth.password_change", "success")
	return handler.respondPasswordChanged(c)
}

// StartLocalPasswordSetupReauth begins the OIDC step-up flow that lets an
// OIDC-only account enroll a local password. It validates the new password
// pair, prepares the bcrypt hash, stashes it in a sealed step-up cookie, and
// returns a redirect URL pointing at the provider authorize endpoint with
// prompt=login + max_age=0 so the provider is forced to re-authenticate the
// user interactively. Nothing is written to the database here.
func (handler *Handler) StartLocalPasswordSetupReauth(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "auth.local_password_setup.start", spec)
		return handler.respondMappedError(c, spec)
	}
	if user.LocalAuthEnabled {
		spec := settingsInvalidInputErrorSpec()
		handler.logSecurityError(c, "auth.local_password_setup.start", spec)
		return handler.respondMappedError(c, spec)
	}
	if handler.oidcService == nil || !handler.oidcService.Enabled() {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.local_password_setup.start", spec)
		return handler.respondMappedError(c, spec)
	}

	input, err := parseChangePasswordInput(c)
	if err != nil {
		spec := settingsInvalidInputErrorSpec()
		handler.logSecurityError(c, "auth.local_password_setup.start", spec)
		return handler.respondMappedError(c, spec)
	}

	preparedHash, err := handler.settingsService.PrepareLocalPasswordHash(user, input.NewPassword, input.ConfirmPassword)
	if err != nil {
		return handler.respondPasswordChangeError(c, err)
	}

	state, err := newOIDCStepupState(time.Now(), oidcStepupPurposeLocalPasswordSetup, user.ID, preparedHash)
	if err != nil {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.local_password_setup.start", spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.setOIDCStepupCookie(c, state); err != nil {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.local_password_setup.start", spec)
		return handler.respondMappedError(c, spec)
	}
	// Drop any in-flight ordinary login state — at the callback we will look
	// for the stepup cookie first, but two competing flows for the same user
	// are an indicator of confusion at best and CSRF at worst.
	handler.clearOIDCStateCookie(c)

	ctx, cancel := oidcRequestContext(c)
	defer cancel()

	authURL, err := handler.oidcService.StartReauth(ctx, state.State, state.Nonce, state.CodeVerifier)
	if err != nil {
		handler.clearOIDCStepupCookie(c)
		spec := mapAuthOIDCError(err)
		handler.logSecurityError(c, "auth.local_password_setup.start", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, "auth.local_password_setup.start", "redirect_issued")
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "redirect_url": authURL})
	}
	return c.Redirect(authURL, fiber.StatusSeeOther)
}

// completeLocalPasswordSetupReauth is dispatched from CompleteOIDCLogin when
// the request carries a valid step-up cookie instead of (or in preference to)
// the ordinary login state cookie. It must verify the OIDC exchange against
// the same user that initiated the flow before committing the prepared
// password.
func (handler *Handler) completeLocalPasswordSetupReauth(c *fiber.Ctx, state oidcStepupState) error {
	if state.Purpose != oidcStepupPurposeLocalPasswordSetup {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, "auth.local_password_setup.callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/settings", fiber.StatusSeeOther)
	}

	// /auth/oidc/callback runs without AuthRequired middleware (the ordinary
	// login path needs to work for unauthenticated visitors), so we resolve
	// the current session from the auth cookie ourselves.
	user, err := handler.authenticateRequest(c)
	if err != nil || user == nil || user.ID != state.UserID {
		spec := settingsOIDCReauthMismatchErrorSpec()
		handler.logSecurityError(c, "auth.local_password_setup.callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/settings", fiber.StatusSeeOther)
	}
	if user.LocalAuthEnabled {
		// Another flow finished first; nothing left to do.
		return c.Redirect("/settings", fiber.StatusSeeOther)
	}

	callbackState := c.FormValue("state")
	code := c.FormValue("code")
	if !state.matchesState(callbackState) {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, "auth.local_password_setup.callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/settings", fiber.StatusSeeOther)
	}
	if c.FormValue("error") != "" {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.local_password_setup.callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/settings", fiber.StatusSeeOther)
	}

	ctx, cancel := oidcRequestContext(c)
	defer cancel()
	if err := handler.validateLocalPasswordSetupReauth(ctx, code, state.CodeVerifier, state.Nonce, user.ID, time.Now()); err != nil {
		spec := mapLocalPasswordSetupReauthError(err)
		handler.logSecurityError(c, "auth.local_password_setup.callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/settings", fiber.StatusSeeOther)
	}

	recoveryCode, err := handler.settingsService.FinalizeLocalPasswordSetup(c.UserContext(), user, state.PasswordHash)
	if err != nil {
		return handler.respondPasswordChangeError(c, err)
	}
	if err := handler.refreshPasswordChangeSession(c, user); err != nil {
		return err
	}

	handler.logSecurityEvent(c, "auth.local_password_setup.callback", "success")
	return handler.renderRecoveryCodeResponseWithContinuePath(c, user, recoveryCode, fiber.StatusOK, "/settings")
}

func (handler *Handler) validateLocalPasswordSetupReauth(ctx context.Context, code, codeVerifier, nonce string, userID uint, now time.Time) error {
	return handler.oidcService.ValidateReauthExchange(ctx, code, codeVerifier, nonce, userID, stepupReauthMaxAge, now)
}

func mapLocalPasswordSetupReauthError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrOIDCReauthStale):
		return settingsOIDCReauthStaleErrorSpec()
	case errors.Is(err, services.ErrOIDCReauthIdentityMismatch):
		return settingsOIDCReauthMismatchErrorSpec()
	case errors.Is(err, services.ErrOIDCCallbackInvalid):
		return authOIDCAuthenticationFailedErrorSpec()
	case errors.Is(err, services.ErrOIDCAuthenticationFailed):
		return authOIDCAuthenticationFailedErrorSpec()
	case errors.Is(err, services.ErrOIDCDisabled), errors.Is(err, services.ErrOIDCUnavailable):
		return authOIDCUnavailableErrorSpec()
	default:
		return authOIDCAuthenticationFailedErrorSpec()
	}
}

func parseChangePasswordInput(c *fiber.Ctx) (changePasswordInput, error) {
	input := changePasswordInput{}
	if err := c.BodyParser(&input); err != nil {
		return changePasswordInput{}, err
	}
	return input, nil
}

func (handler *Handler) refreshPasswordChangeSession(c *fiber.Ctx, user *models.User) error {
	return handler.refreshCurrentSession(c, user, "auth.password_change")
}

func (handler *Handler) respondPasswordChangeError(c *fiber.Ctx, err error) error {
	spec := mapSettingsPasswordChangeError(err)
	handler.logSecurityError(c, "auth.password_change", spec)
	return handler.respondMappedError(c, spec)
}

func (handler *Handler) respondPasswordChanged(c *fiber.Ctx) error {
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		messageKey := services.SettingsStatusTranslationKey("password_changed")
		message := translateMessage(currentMessages(c), messageKey)
		if message == "" || message == messageKey {
			message = "Password changed successfully."
		}
		return c.SendString(htmxDismissibleSuccessStatusMarkup(currentMessages(c), message))
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "password_changed"})
	return redirectOrJSON(c, "/settings")
}
