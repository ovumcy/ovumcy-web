package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) SetLanguage(c fiber.Ctx) error {
	languageInput := strings.TrimSpace(c.FormValue("lang"))
	if languageInput == "" {
		return fiber.ErrBadRequest
	}

	language := handler.i18n.NormalizeLanguage(languageInput)
	if spec, ok := handler.persistSwitchedLanguage(c, languageInput, language); !ok {
		return handler.respondMappedError(c, spec)
	}
	handler.setLanguageCookie(c, language)

	nextPath := services.SanitizeRedirectPath(c.FormValue("next"), "/")
	if isHTMX(c) {
		c.Set("HX-Redirect", nextPath)
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To(nextPath)
}

// persistSwitchedLanguage stores the switched language on the account when the
// request carries a valid session, through the same service call `/interface`
// uses. Without it the switch lived only in the cookie, and every later session
// issue re-wrote that cookie from the account column (setAuthCookie →
// applyStoredLanguage) — so a signed-in owner switching language here, most
// visibly during onboarding where this is the only switcher on the page, had the
// choice silently reverted on the next re-issue.
//
// It reports (spec, ok): false means the response has to be the mapped error the
// spec names — the same failure `/interface` reports, never an inline status
// pair, and never a cookie-only degradation that leaves the two stores holding
// different languages behind a redirect that looked like success.
//
// The unauthenticated path is untouched: no session, no account write, and the
// answer is byte-identical to the authenticated one, so nothing here tells a
// caller whether the cookie they sent names a live session.
//
// The stored half is gated on the SUBMITTED code, not on the normalized one.
// NormalizeLanguage answers with the operator default for anything it does not
// ship, so persisting its output would pin an account to a language nobody
// picked — the refusal `/interface` performs, expressed where this route cannot
// borrow its status: refusing outright would change the answer an unauthenticated
// caller has always received for the same input. An unsupported code therefore
// stays exactly what it was, a cookie-only fallback to the default.
func (handler *Handler) persistSwitchedLanguage(c fiber.Ctx, submitted string, normalized string) (APIErrorSpec, bool) {
	if !handler.i18n.IsSupportedLanguage(submitted) {
		return APIErrorSpec{}, true
	}
	user := handler.optionalAuthenticatedUser(c)
	if user == nil {
		return APIErrorSpec{}, true
	}
	if err := handler.settingsService.SaveInterfaceLanguage(c.Context(), user.ID, normalized); err != nil {
		return settingsInterfaceUpdateErrorSpec(), false
	}
	return APIErrorSpec{}, true
}

func (handler *Handler) ShowPrivacyPage(c fiber.Ctx) error {
	messages := currentMessages(c)
	authenticatedUser := handler.optionalAuthenticatedUser(c)
	data := buildPrivacyPageData(messages, c.Query("back"), authenticatedUser)
	return handler.render(c, "privacy", data)
}
