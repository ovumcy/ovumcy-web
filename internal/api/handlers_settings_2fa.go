package api

import (
	"bytes"
	"encoding/base64"
	"html/template"
	"image/png"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const totpIssuer = "Ovumcy"

// ShowTOTPSetupPage renders the TOTP enrollment or management page.
// If TOTP is already enabled it shows the management view (status + disable button).
// If not enabled it generates a new key, stores the raw secret in a short-lived
// sealed cookie, and renders the QR code + manual secret.
func (handler *Handler) ShowTOTPSetupPage(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	messages := currentMessages(c)
	data := fiber.Map{
		"Title":       localizedPageTitle(messages, "settings.2fa.title", "Ovumcy | Two-Factor Authentication"),
		"CurrentUser": user,
		"TOTPEnabled": user.TOTPEnabled,
	}

	if user.TOTPEnabled {
		return handler.render(c, "settings_2fa", data)
	}

	key, err := handler.totpService.GenerateSetupKey(totpIssuer, user.Email)
	if err != nil {
		handler.logSecurityEvent(c, "settings.2fa.setup", "keygen_failed")
		return handler.respondMappedError(c, settingsLoadErrorSpec())
	}

	// Generate QR code PNG and encode as base64 data URL.
	img, err := key.Image(200, 200)
	if err != nil {
		handler.logSecurityEvent(c, "settings.2fa.setup", "qr_failed")
		return handler.respondMappedError(c, settingsLoadErrorSpec())
	}
	var qrBuf bytes.Buffer
	if err := png.Encode(&qrBuf, img); err != nil {
		handler.logSecurityEvent(c, "settings.2fa.setup", "qr_encode_failed")
		return handler.respondMappedError(c, settingsLoadErrorSpec())
	}
	qrDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(qrBuf.Bytes())

	// Persist the raw secret in a short-lived sealed cookie so it survives the
	// form submission without touching the database before the user confirms.
	if err := handler.setTOTPSetupCookie(c, user.ID, key.Secret()); err != nil {
		handler.logSecurityEvent(c, "settings.2fa.setup", "cookie_failed")
		return handler.respondMappedError(c, settingsLoadErrorSpec())
	}

	data["QRDataURL"] = template.URL(qrDataURL) // #nosec G203 -- server-built data: URI from a server-rendered PNG, no user input
	data["TOTPSecret"] = key.Secret()
	return handler.render(c, "settings_2fa", data)
}

// VerifyTOTP2FAEnrollment confirms TOTP enrollment by validating the user-supplied
// code against the secret held in the setup cookie, then persists the encrypted
// secret and marks TOTP as enabled.
func (handler *Handler) VerifyTOTP2FAEnrollment(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	if _, spec, valid := handler.validateSettingsActionPassword(c); !valid {
		handler.logSecurityError(c, "settings.2fa.verify", spec)
		return handler.respondMappedError(c, spec)
	}

	// The pending secret is only enrollable by the account it was generated for.
	// A cookie that names a different account, or none, is refused — as is one
	// that cannot be opened, parsed, or has expired — and the reader clears it on
	// every one of those branches, so it cannot be replayed onto this session on
	// a retry. All of them map to the same answer: the response must not tell an
	// attacker whether the enrollment expired or was minted for someone else.
	rawSecret, err := handler.parseTOTPSetupCookie(c, user.ID)
	if err != nil {
		return handler.respondMappedError(c, totpSessionExpiredErrorSpec())
	}

	code := strings.TrimSpace(c.FormValue("code"))
	if len(code) != 6 {
		return handler.respondMappedError(c, totpInvalidCodeErrorSpec())
	}

	if !handler.totpService.ValidateCodeRaw(rawSecret, code) {
		handler.logSecurityError(c, "settings.2fa.verify", totpInvalidCodeErrorSpec())
		return handler.respondMappedError(c, totpInvalidCodeErrorSpec())
	}

	if err := handler.totpService.EnableTOTP(c.Context(), user.ID, rawSecret); err != nil {
		handler.logSecurityError(c, "settings.2fa.verify", totpInternalErrorSpec())
		return handler.respondMappedError(c, totpInternalErrorSpec())
	}

	// EnableTOTP atomically bumped auth_session_version on the user row; mirror
	// the bump in memory and re-issue the auth cookie so this device stays
	// signed in while every other session that existed before 2FA was enabled
	// is invalidated on its next request.
	user.AuthSessionVersion = services.NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	user.TOTPEnabled = true
	if spec, ok := handler.refreshCurrentSession(c, user, "settings.2fa.verify"); !ok {
		// The setup cookie is cleared HERE and not only in the success arm
		// below. EnableTOTP has already persisted the encrypted secret, so the
		// enrollment seed this sealed cookie carries is spent — and stopping at
		// this refusal skips the clear that used to run when the dead guard fell
		// through. Left in place it would ride every request for the rest of the
		// browser session, past the moment its own scope ends: the seed is held
		// in the setup cookie only until the first code verifies, and by this
		// line it has verified.
		handler.clearTOTPSetupCookie(c)
		// This route answers on its own path, so respondMappedError is the right
		// channel — but the answer only stands if the handler stops here. The
		// success arm below writes an HTMX toast or a 303 over whatever was
		// already in the response.
		return handler.respondMappedError(c, spec)
	}

	handler.clearTOTPSetupCookie(c)
	handler.logSecurityEvent(c, "settings.2fa.verify", "enabled")

	if isHTMX(c) {
		messages := currentMessages(c)
		return c.Status(fiber.StatusOK).SendString(
			htmxDismissibleSuccessStatusMarkup(messages, translateMessage(messages, "settings.2fa.enabled_status")),
		)
	}
	// The mirror image of the refusal side of this branch: a verdict has to be
	// written to a channel its destination reads. /settings/2fa builds its
	// template data inline and never pops the flash cookie, so a confirmation
	// left here was invisible and rode along until some later page consumed it.
	// The redirect goes to /settings — the one page that reads the flash and
	// renders it through the single status island — rather than teaching a
	// second page to read it: /settings also shows the new 2FA state in its
	// account card, so the confirmation and the state it is about arrive
	// together. The flashed value is a status SLUG, never a translation key:
	// the island resolves it through services.SettingsStatusTranslationKey, and
	// an unmapped value renders an empty banner.
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "two_factor_enabled"})
	return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
}

// DisableTOTP2FA disables TOTP for the current user after verifying their password.
func (handler *Handler) DisableTOTP2FA(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	password := c.FormValue("password")
	if strings.TrimSpace(password) == "" {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}

	if err := handler.totpService.CheckDisableRateLimit(handler.secretKey, c.IP(), user.ID, time.Now()); err != nil {
		spec := totpDisableRateLimitedErrorSpec()
		handler.logSecurityError(c, "settings.2fa.disable", spec)
		return handler.respondMappedError(c, spec)
	}

	if _, err := handler.authService.AuthenticateCredentials(c.Context(), user.Email, password); err != nil {
		handler.totpService.RecordDisableFailure(handler.secretKey, c.IP(), user.ID, time.Now())
		spec := authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials")
		handler.logSecurityError(c, "settings.2fa.disable", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.totpService.ResetDisableAttempts(handler.secretKey, c.IP(), user.ID)

	if err := handler.totpService.DisableTOTP(c.Context(), user.ID); err != nil {
		handler.logSecurityError(c, "settings.2fa.disable", totpInternalErrorSpec())
		return handler.respondMappedError(c, totpInternalErrorSpec())
	}

	// DisableTOTP bumped auth_session_version atomically; mirror the bump in
	// memory and refresh this device's cookie so every other session that
	// existed while 2FA was on is invalidated.
	user.AuthSessionVersion = services.NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	user.TOTPEnabled = false
	user.TOTPSecret = ""
	if spec, ok := handler.refreshCurrentSession(c, user, "settings.2fa.disable"); !ok {
		// Same stop-here reason as the enable arm above.
		//
		// codecov:ignore:start -- owner-only route: only the AEAD seal error is
		// left, and no request-shaped input provokes it.
		return handler.respondMappedError(c, spec)
		// codecov:ignore:end
	}

	handler.logSecurityEvent(c, "settings.2fa.disable", "disabled")

	if isHTMX(c) {
		messages := currentMessages(c)
		return c.Status(fiber.StatusOK).SendString(
			htmxDismissibleSuccessStatusMarkup(messages, translateMessage(messages, "settings.2fa.disabled_status")),
		)
	}
	// Same destination and the same slug rule as the enable arm above; fixing
	// one of the two would leave the other rendering nothing. Landing on
	// /settings rather than back here also stops a disable from re-entering the
	// enrollment arm of ShowTOTPSetupPage, which would mint a fresh TOTP seed
	// and a new setup cookie for an owner who just asked for the opposite.
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "two_factor_disabled"})
	return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
}
