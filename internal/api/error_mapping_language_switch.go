package api

import (
	"fmt"
	"html/template"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// languageSwitchBackLabelKey labels the way back out of a refused language
// switch.
const languageSwitchBackLabelKey = "common.back"

// isLanguageSwitchPageNavigation reports whether c is a plain HTML navigation
// submitting the language-switch form — the app's one public form with no HTMX
// and no JavaScript behind it. Its refusal replaces the whole page, so whichever
// layer raises it (the handler, CSRF, a recovered panic, the request deadline)
// a JSON envelope would be painted into the browser window as text.
//
// Scoped to POST because the route and its limiter are POST-only: any other
// method on the path is an unrouted request and answers like one everywhere else.
func isLanguageSwitchPageNavigation(c fiber.Ctx) bool {
	return c.Method() == fiber.MethodPost &&
		httpx.RoutingNormalizedPath(c.Path()) == LanguageSwitchPath &&
		responseFormat(c) == httpx.ResponseFormatHTML
}

// sendLanguageSwitchStatusFragment answers one mapped spec as the shared status
// fragment followed by a link back to the form's sanitized `next` path: the
// fragment is the whole page the browser shows, and without the link a refused
// switch — most often an idle CSRF token — is a dead end. Status and stable key
// still come from the spec.
func sendLanguageSwitchStatusFragment(c fiber.Ctx, spec APIErrorSpec) error {
	back := services.SanitizeRedirectPath(c.FormValue("next"), "/")
	label := languageSwitchBackLabelKey
	if localized, translated := lookupMessage(currentMessages(c), languageSwitchBackLabelKey); translated {
		label = localized
	}
	markup := localizedStatusErrorMarkup(c, spec) + fmt.Sprintf(
		"<p><a href=\"%s\">%s</a></p>",
		template.HTMLEscapeString(back),
		template.HTMLEscapeString(label),
	)
	return sendHTMLFragment(c.Status(spec.Status), markup)
}
