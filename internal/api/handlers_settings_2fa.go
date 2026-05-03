package api

import (
	"bytes"
	"encoding/base64"
	"html/template"
	"image/png"
	"strings"

	"github.com/gofiber/fiber/v2"
)

const totpIssuer = "Ovumcy"

// ShowTOTPSetupPage renders the TOTP enrollment or management page.
// If TOTP is already enabled it shows the management view (status + disable button).
// If not enabled it generates a new key, stores the raw secret in a short-lived
// sealed cookie, and renders the QR code + manual secret.
func (handler *Handler) ShowTOTPSetupPage(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return c.Redirect("/login", fiber.StatusSeeOther)
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
	if err := handler.setTOTPSetupCookie(c, key.Secret()); err != nil {
		handler.logSecurityEvent(c, "settings.2fa.setup", "cookie_failed")
		return handler.respondMappedError(c, settingsLoadErrorSpec())
	}

	data["QRDataURL"] = template.URL(qrDataURL) //nolint:gosec // data URI for inline QR image
	data["TOTPSecret"] = key.Secret()
	return handler.render(c, "settings_2fa", data)
}

// VerifyTOTP2FAEnrollment confirms TOTP enrollment by validating the user-supplied
// code against the secret held in the setup cookie, then persists the encrypted
// secret and marks TOTP as enabled.
func (handler *Handler) VerifyTOTP2FAEnrollment(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	rawSecret, err := handler.parseTOTPSetupCookie(c)
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

	if err := handler.totpService.EnableTOTP(user.ID, rawSecret); err != nil {
		handler.logSecurityError(c, "settings.2fa.verify", totpInternalErrorSpec())
		return handler.respondMappedError(c, totpInternalErrorSpec())
	}

	handler.clearTOTPSetupCookie(c)
	handler.logSecurityEvent(c, "settings.2fa.verify", "enabled")

	if isHTMX(c) {
		messages := currentMessages(c)
		return c.Status(fiber.StatusOK).SendString(
			htmxDismissibleSuccessStatusMarkup(messages, translateMessage(messages, "settings.2fa.enabled_status")),
		)
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "settings.2fa.enabled_status"})
	return c.Redirect("/settings/2fa", fiber.StatusSeeOther)
}

// DisableTOTP2FA disables TOTP for the current user after verifying their password.
func (handler *Handler) DisableTOTP2FA(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	password := c.FormValue("password")
	if strings.TrimSpace(password) == "" {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}

	if _, err := handler.authService.AuthenticateCredentials(user.Email, password); err != nil {
		handler.logSecurityError(c, "settings.2fa.disable", authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials"))
		return handler.respondMappedError(c, authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials"))
	}

	if err := handler.totpService.DisableTOTP(user.ID); err != nil {
		handler.logSecurityError(c, "settings.2fa.disable", totpInternalErrorSpec())
		return handler.respondMappedError(c, totpInternalErrorSpec())
	}

	handler.logSecurityEvent(c, "settings.2fa.disable", "disabled")

	if isHTMX(c) {
		messages := currentMessages(c)
		return c.Status(fiber.StatusOK).SendString(
			htmxDismissibleSuccessStatusMarkup(messages, translateMessage(messages, "settings.2fa.disabled_status")),
		)
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "settings.2fa.disabled_status"})
	return c.Redirect("/settings/2fa", fiber.StatusSeeOther)
}
