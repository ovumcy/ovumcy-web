package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Egress ledger — the owner-only section's mutation transport.
//
// One endpoint is new here: DELETE /api/v1/users/current/webhook, which
// withdraws the stored destination. It exists because the only previous way to
// remove one was to submit the whole settings form with a "remove" checkbox
// ticked, which meant an owner could not withdraw an endpoint this instance can
// no longer decrypt — the form's save path validates and re-encrypts a URL, and
// the row it needed to act on is exactly the one that cannot be read.
//
// The write behind it touches the endpoint columns and nothing else. Routing it
// through the shared save would have written reminder_lead_days as well, and a
// zero lead window makes a reminder due on exactly one calendar day with no
// later pass to retry it: a "remove my webhook" click would silently cost the
// owner their next banner reminder too.
//
// Nothing on this path decrypts webhook_url. That is what keeps the unreadable
// state actionable rather than a trap.

var webhookRemoveMutation = healthMutationKind{action: "settings.webhook_remove", target: "webhook"}

// RemoveWebhookDestination withdraws the owner's stored delivery endpoint.
// Transport only: the service owns the write, the revocation-epoch advance, and
// the clearing of the delivery mark.
func (handler *Handler) RemoveWebhookDestination(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, webhookRemoveMutation, unauthorizedErrorSpec())
	}

	if err := handler.webhookSettingsSvc.RemoveWebhookDestination(c.Context(), user.ID); err != nil {
		return handler.failMutation(c, webhookRemoveMutation, settingsWebhookUpdateErrorSpec())
	}

	handler.logMutationSuccess(c, webhookRemoveMutation)
	return handler.respondEgressMutation(c, user, services.SettingsWebhookRemovedStatus)
}

// respondEgressMutation answers a completed egress mutation. For an HTMX caller
// it re-renders the whole state block, REBUILT from a repository read issued
// after the write — never from what the request asked for.
//
// The distinction is not academic. A response assembled from the caller's intent
// swaps in a fresh success toast beside a state sentence the write just
// falsified, and it does so precisely when the write succeeded, which is when
// nobody looks. Regression:
// TestEveryEgressMutationRebuildsItsBlockFromAReadAfterTheWrite.
func (handler *Handler) respondEgressMutation(c fiber.Ctx, user *models.User, status string) error {
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "status": status})
	}
	if isHTMX(c) {
		data, err := handler.buildSettingsEgressBlockData(c, user, status)
		if err != nil {
			return handler.respondMappedError(c, settingsLoadErrorSpec())
		}
		c.Status(fiber.StatusOK)
		return handler.renderPartial(c, "settings_egress_section", data)
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: status})
	return redirectOrJSON(c, "/settings")
}

// buildSettingsEgressBlockData assembles the swap payload: the rebuilt ledger
// plus the one status key the single island announces. The status rides the same
// swap as the state it is about, so the two cannot be delivered out of step.
func (handler *Handler) buildSettingsEgressBlockData(c fiber.Ctx, user *models.User, status string) (fiber.Map, error) {
	ledger, err := handler.settingsViewService.BuildSettingsEgressViewData(c.Context(), user)
	if err != nil {
		return nil, err
	}
	return fiber.Map{
		"Egress":          buildSettingsEgressView(c, ledger),
		"EgressStatusKey": services.SettingsStatusTranslationKey(status),
	}, nil
}
