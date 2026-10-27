package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

const (
	totpPanelEnabled  = "enabled"
	totpPanelDisabled = "disabled"
)

// respondTOTPPanel answers a committed enable or disable from an htmx form with
// the two-factor panel rebuilt from a read taken AFTER the write, so the card
// that said "set up" or "disable" cannot stay standing beside a success message
// about the opposite state.
//
// The forms keep the status island as their own target, which is what a
// refusal needs (the enrollment form cannot be rebuilt on a refusal: its seed
// lives only in the sealed setup cookie). A committed write retargets the
// response to the panel instead.
//
// The panel never carries an enrollment seed here. After a disable it shows the
// "not enabled" state with a link to the setup page: the seed is minted fresh on
// that page's own visit and sealed in the setup cookie, and this response mints
// none.
func (handler *Handler) respondTOTPPanel(c fiber.Ctx, user *models.User, outcome string) error {
	fresh, err := handler.authService.FindByID(c.Context(), user.ID)
	if err != nil {
		return handler.respondMappedError(c, settingsLoadErrorSpec())
	}
	messages := currentMessages(c)
	statusKey := "settings.2fa.disabled_status"
	if outcome == totpPanelEnabled {
		statusKey = "settings.2fa.enabled_status"
	}
	data := fiber.Map{
		"CurrentUser":           &fresh,
		"TOTPEnabled":           fresh.TOTPEnabled,
		"LocalPasswordRequired": !fresh.LocalAuthEnabled,
		"PanelOutcome":          outcome,
		"PanelStatus":           httpx.DismissibleStatusOKTemplateHTML(translateMessage(messages, statusKey), localizedStatusDismissLabel(messages)),
	}
	c.Set("HX-Retarget", "#settings-2fa-panel")
	c.Set("HX-Reswap", "outerHTML")
	c.Status(fiber.StatusOK)
	return handler.renderPartial(c, "settings_2fa_panel_section", data)
}
