package api

import (
	"bytes"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// dayFormAccountField is the hidden field every rendered day write carries
// (the dashboard journal, the calendar day editor, both delete controls and
// every cycle-start form): an opaque binding to the account that rendered the
// page (security.DayFormAccountBinding). dayFormAccountHeader carries the same
// value for a client request that sends no form body (the dashboard autosave's
// undo is a DELETE with headers only).
//
// The binding exists because a day write refused for an expired session offers
// sign-in in a new tab and then a retry of the same form, and because any page
// left open keeps its controls after a different account signs in elsewhere in
// the same browser. The session cookie is browser-wide and the CSRF token is
// not bound to an account, so without the binding an old tab would save,
// delete or mark a cycle start on the SECOND account's day. A write whose
// binding names another account is refused with 409 and nothing is written.
//
// A request from a rendered page (HTMX, or a form post that accepts text/html)
// without any binding is refused the same way: a page that lost the field must
// not disable the check. Only a client that never renders a page — a JSON API
// caller — may omit it, and its writes keep the behaviour they had.
const (
	dayFormAccountField  = "day_form_account"
	dayFormAccountHeader = "X-Ovumcy-Day-Form-Account"
)

func dayFormAccountChangedErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "day form account changed")
}

// dayFormAccountBinding is the value the day forms render for user.
func (handler *Handler) dayFormAccountBinding(user *models.User) (string, error) {
	return security.DayFormAccountBinding(handler.secretKey, user.ID)
}

// RefuseDayFormFromAnotherAccount returns the middleware every day write route
// mounts after OwnerOnly, refusing as kind. Every binding the request carries —
// each urlencoded body value, each multipart value and each header value, the
// field name matched in any letter case — must be the binding of the account
// now signed in, compared in constant time: one valid copy cannot mask a
// mismatching one. No binding at all passes only for a request that did not
// come from a rendered page.
func (handler *Handler) RefuseDayFormFromAnotherAccount(kind healthMutationKind) fiber.Handler {
	return func(c fiber.Ctx) error {
		user, ok := currentUser(c)
		if !ok {
			return handler.failDayMutation(c, kind, unauthorizedErrorSpec()) // codecov:ignore -- OwnerOnly precedes this on every route that mounts it
		}
		presented := presentedDayFormAccountBindings(c)
		if len(presented) == 0 && dayWriteFromRenderedPage(c) {
			return handler.failDayMutation(c, kind, dayFormAccountChangedErrorSpec())
		}
		if !dayFormRenderedForAccount(handler.secretKey, user, presented) {
			return handler.failDayMutation(c, kind, dayFormAccountChangedErrorSpec())
		}
		return c.Next()
	}
}

// presentedDayFormAccountBindings collects every copy of the binding the
// request carries, from every source a client can send it in.
func presentedDayFormAccountBindings(c fiber.Ctx) [][]byte {
	var presented [][]byte
	field := []byte(dayFormAccountField)
	c.RequestCtx().PostArgs().All()(func(key, value []byte) bool {
		if bytes.EqualFold(key, field) {
			presented = append(presented, value)
		}
		return true
	})
	if form, err := c.MultipartForm(); err == nil && form != nil {
		for name, values := range form.Value {
			if !strings.EqualFold(name, dayFormAccountField) {
				continue
			}
			for _, value := range values {
				presented = append(presented, []byte(value))
			}
		}
	}
	presented = append(presented, c.Request().Header.PeekAll(dayFormAccountHeader)...)
	return presented
}

// dayWriteFromRenderedPage reports whether the request came from a page the
// server rendered: an HTMX request, or a form post (urlencoded or multipart)
// whose Accept names text/html.
func dayWriteFromRenderedPage(c fiber.Ctx) bool {
	if isHTMX(c) {
		return true
	}
	contentType := strings.ToLower(c.Get(fiber.HeaderContentType))
	formBody := strings.HasPrefix(contentType, fiber.MIMEApplicationForm) || strings.HasPrefix(contentType, fiber.MIMEMultipartForm)
	return formBody && strings.Contains(strings.ToLower(c.Get(fiber.HeaderAccept)), "text/html")
}

// dayFormRenderedForAccount reports whether every presented binding names
// user. It is called with no binding only for a request that did not come from
// a rendered page.
func dayFormRenderedForAccount(secretKey []byte, user *models.User, presented [][]byte) bool {
	for _, value := range presented {
		if !security.DayFormAccountBindingMatches(secretKey, user.ID, string(value)) {
			return false
		}
	}
	return true
}
