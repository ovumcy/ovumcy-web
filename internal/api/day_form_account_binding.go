package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// dayFormAccountField is the hidden field both day forms (the dashboard journal
// and the calendar day editor) carry: an opaque binding to the account that
// rendered the form (security.DayFormAccountBinding).
//
// The binding exists because a day save refused for an expired session offers
// sign-in in a new tab and then a retry of the same form. The session cookie is
// browser-wide and the CSRF token is not bound to an account, so if a DIFFERENT
// account signs in in that tab, the retry would otherwise write the first
// person's entry into the second account. A form whose binding names another
// account is refused with 409 and nothing is written; the entry stays in the
// form that sent it.
//
// A request without the field is not refused: a JSON client, or a form built by
// hand, never rendered a form for any account, and its writes keep the
// behaviour they had.
const dayFormAccountField = "day_form_account"

func dayFormAccountChangedErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "day form account changed")
}

// dayFormAccountBinding is the value the day forms render for user.
func (handler *Handler) dayFormAccountBinding(user *models.User) (string, error) {
	return security.DayFormAccountBinding(handler.secretKey, user.ID)
}

// RefuseDayFormFromAnotherAccount runs on the day upsert route after OwnerOnly.
// Every value the body carries under dayFormAccountField must be the binding of
// the account now signed in, compared in constant time; a repeated field is
// held to the same rule per value, so a second copy cannot outvote the first.
func (handler *Handler) RefuseDayFormFromAnotherAccount(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failDayMutation(c, dayUpsertMutation, unauthorizedErrorSpec()) // codecov:ignore -- OwnerOnly precedes this on the only route that mounts it
	}
	if !dayFormRenderedForAccount(handler.secretKey, user, c.RequestCtx().PostArgs().PeekMulti(dayFormAccountField)) {
		return handler.failDayMutation(c, dayUpsertMutation, dayFormAccountChangedErrorSpec())
	}
	return c.Next()
}

// dayFormRenderedForAccount reports whether every presented binding names
// user. No binding at all is the field-absent case above and passes.
func dayFormRenderedForAccount(secretKey []byte, user *models.User, presented [][]byte) bool {
	for _, value := range presented {
		if !security.DayFormAccountBindingMatches(secretKey, user.ID, string(value)) {
			return false
		}
	}
	return true
}
