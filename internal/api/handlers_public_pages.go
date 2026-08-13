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
		// The page-form arm, not the bare envelope: this is the application's one
		// public form with no HTMX and no JavaScript behind it, so a refusal is a
		// full-page navigation and a JSON body would be painted into the browser
		// window. Same shape the limiter refusal on this path already uses; HTMX
		// and JSON callers keep the envelope, and the status and key still come
		// from the mapped spec.
		return handler.respondPageFormMappedError(c, spec)
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
// The unauthenticated path is untouched: no session, no account write, and no
// difference in the answer. A caller with NO cookie and an authenticated owner
// receive the same status, body and language cookie. A caller presenting a
// stale or unopenable `ovumcy_auth` additionally gets that dead cookie
// retracted, because resolving the session is what discovers it is dead — a
// self-directed clean-up of the value the caller itself sent, not a signal about
// any account, and the same one every other optional-auth page already emits.
//
// The stored half is gated on the SUBMITTED code, not on the normalized one.
// NormalizeLanguage answers with the operator default for anything it does not
// ship, so persisting its output would pin an account to a language nobody
// picked. The predicate is the one `/interface` refuses on, `IsSupportedLanguage`
// — but the consequence here is not a refusal: this route's answer must not
// depend on whether the caller holds a session, and it has always accepted an
// unsupported code as a cookie-only fallback to the default. So an unsupported
// code reaches neither the account nor the cookie in the form it was submitted.
//
// The owner-role check is defense in depth, not a live branch: ResolveAuthSession
// already refuses an unsupported role, so optionalAuthenticatedUser answers nil
// for one. It is here for the same reason every mutating route declares
// `handler.OwnerOnly` on top of `AuthRequired` — this is a state-changing write
// on a route that carries neither middleware, so it applies the same predicate
// itself rather than inheriting one transitively. Measured, not assumed: deleting
// this clause leaves TestUnsupportedRoleLanguageSwitchStoresNothing green,
// because the resolver's own refusal rescues the property. That is what a second
// layer looks like from the outside, and it is the reason the clause has to be
// read as an invariant rather than as a branch a test can kill.
func (handler *Handler) persistSwitchedLanguage(c fiber.Ctx, submitted string, normalized string) (APIErrorSpec, bool) {
	if !handler.i18n.IsSupportedLanguage(submitted) {
		return APIErrorSpec{}, true
	}
	user := handler.optionalAuthenticatedUser(c)
	if user == nil || !services.IsOwnerUser(user) {
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
