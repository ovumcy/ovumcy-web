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
		return handler.failEgressMutation(c, webhookRemoveMutation, user, settingsWebhookUpdateErrorSpec())
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
	return handler.respondEgressMutationHTML(c, user, status)
}

// respondEgressMutationHTML is the browser half on its own, for the one caller
// that answers JSON with more than {ok, status} and would otherwise carry a
// second, unreachable JSON branch below its own.
func (handler *Handler) respondEgressMutationHTML(c fiber.Ctx, user *models.User, status string) error {
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

// failEgressMutation is failMutation for the five mutations that answer with
// this card, and it exists because the shared settings-error transport cannot
// serve one.
//
// That transport answers an HTMX caller with 200 and a BARE status fragment,
// which htmx then swaps into hx-target. That was right while every form targeted
// its own status island. This card targets ITSELF with outerHTML, so the bare
// fragment replaced the entire section: the refusal ended up standing alone in
// the page where the card had been, and the island the refusal was written for
// no longer existed. It is the success path's own defect wearing the other
// outcome, and it is fixed the same way — the answer is the card, rebuilt from a
// read, with the refusal inside its island.
//
// Non-HTMX callers keep the transport they had: JSON gets the typed error, a
// plain form post gets the flash and the redirect. Regression:
// TestEveryEgressMutationAnswersARefusalWithTheRebuiltCard.
func (handler *Handler) failEgressMutation(c fiber.Ctx, kind healthMutationKind, user *models.User, spec APIErrorSpec) error {
	handler.logMutationError(c, kind, spec)
	if !isHTMX(c) {
		return handler.respondMappedError(c, spec)
	}

	data, err := handler.buildSettingsEgressBlockData(c, user, "")
	if err != nil {
		return handler.respondMappedError(c, settingsLoadErrorSpec())
	}
	data["EgressErrorKey"] = egressErrorMessageKey(spec.Key)
	c.Status(fiber.StatusOK)
	return handler.renderPartial(c, "settings_egress_section", data)
}

// egressErrorMessageKey resolves the spec's raw key to the translation key the
// island renders, by the same lookup respondSettingsError uses. An unmapped key
// is returned unchanged rather than swallowed: a refusal with no copy is a bug
// to see, not one to hide behind an empty island.
func egressErrorMessageKey(specKey string) string {
	if key := services.AuthErrorTranslationKey(specKey); key != "" {
		return key
	}
	return specKey
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
		"Egress":          buildSettingsEgressView(c, ledger, handler.requestLocation(c)),
		"EgressStatusKey": services.SettingsStatusTranslationKey(status),
	}, nil
}
