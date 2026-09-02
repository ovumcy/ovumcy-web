package services

import (
	"context"
	"crypto/subtle"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The egress ledger: one owner-scoped account of every path by which anything
// about an owner can leave this instance, and of what each path can PROVE.
//
// The organizing rule is that a rendered state is a proposition whose falsifier
// the code can detect. "Active" is not such a proposition for a calendar feed:
// the verifier plaintext is never stored and its MAC cannot be recomputed, so a
// selector proves a row exists and nothing else — after a SECRET_KEY rotation
// the row still has a selector and the issued link no longer resolves. Nor is a
// claim watermark a delivery time: it is a cycle anchor written BEFORE the POST
// and restored when the POST fails. Every field below is chosen so that the
// sentence it drives can be shown wrong by something the instance observes.
//
// What is deliberately absent is as load-bearing as what is here. There is no
// count, no "last fetched", no "in use" and no rotation history for the .ics
// feed: polls are unaudited on purpose, and adding a field here is how that
// decision would be reversed by accident. There is no "revoked" either — revoke,
// the restore fence, the rotation sentinel and a recovery-code regeneration all
// clear the same three columns, so "revoked" is indistinguishable from "never
// created" and would be a sentence with no falsifier.

// EgressWebhookState is the webhook path's single mutually exclusive state.
type EgressWebhookState string

const (
	// EgressWebhookNotConfigured — no endpoint ciphertext is stored.
	EgressWebhookNotConfigured EgressWebhookState = "not_configured"
	// EgressWebhookUnreadable — an endpoint is stored and this instance cannot
	// open it. Evaluated BEFORE any toggle: a row whose ciphertext no longer
	// opens cannot deliver whatever its flags say, and ordering the toggles first
	// is how a broken key would stay invisible behind "delivery is off".
	EgressWebhookUnreadable EgressWebhookState = "unreadable"
	// EgressWebhookUnusable — the ciphertext opened and names no host. Distinct
	// from unreadable on purpose: one is a key problem, the other a stored-value
	// problem, and the owner's next action differs.
	EgressWebhookUnusable EgressWebhookState = "unusable"
	// EgressWebhookOutboundDisabled — the endpoint is readable but this instance
	// runs no delivery pass, so nothing will leave by this path whatever the
	// row says. REMINDER_SCHEDULER_ENABLED ships off, so this is the default
	// instance's honest answer.
	EgressWebhookOutboundDisabled EgressWebhookState = "outbound_disabled"
	// EgressWebhookStoredOff — delivery is switched off for this owner.
	EgressWebhookStoredOff EgressWebhookState = "stored_off"
	// EgressWebhookStoredNoKinds — delivery is on and no reminder kind is opted
	// into, which the delivery predicate reads per kind: nothing is due, ever.
	EgressWebhookStoredNoKinds EgressWebhookState = "stored_no_kinds"
	// EgressWebhookArmed — a readable endpoint, delivery on, at least one kind
	// opted in. It names no kind: the state is reached with ONE flag set and the
	// claim predicate pins the opt-ins per kind, so a state that named both would
	// be false for half the rows that reach it.
	EgressWebhookArmed EgressWebhookState = "armed"
)

// EgressWebhookStates enumerates the webhook states in evaluation order. It is
// the order itself, not a list of names: a renderer or a test that walks it
// walks the precedence the state machine applies.
var EgressWebhookStates = []EgressWebhookState{
	EgressWebhookNotConfigured,
	EgressWebhookUnreadable,
	EgressWebhookUnusable,
	EgressWebhookOutboundDisabled,
	EgressWebhookStoredOff,
	EgressWebhookStoredNoKinds,
	EgressWebhookArmed,
}

// EgressFeedState is the .ics feed path's single mutually exclusive state.
//
// None of these says the link works. The three "issued" states differ only in
// what the row's recorded key epoch permits saying, which is the whole of what
// is knowable: a stamp equal to the running epoch means the link was minted
// under the key this instance still holds, so whoever holds it can still be cut
// off by revoking it here.
type EgressFeedState string

const (
	// EgressFeedUnknown — the row could not be read, or this instance could not
	// derive its own key epoch. Not the same as "none": answering "no feed" from
	// a read that never saw the row invites the owner to mint a second link
	// beside one that still works.
	EgressFeedUnknown EgressFeedState = "unknown"
	// EgressFeedNone — no selector is stored.
	EgressFeedNone EgressFeedState = "none"
	// EgressFeedIssuedBeforeRecorded — a link exists, minted before the epoch was
	// recorded (the column is not backfilled). Nothing can be said about which
	// key it was minted under.
	EgressFeedIssuedBeforeRecorded EgressFeedState = "issued_before_recorded"
	// EgressFeedIssuedPreviousKey — a link exists and was minted under a key this
	// instance no longer runs.
	EgressFeedIssuedPreviousKey EgressFeedState = "issued_previous_key"
	// EgressFeedIssuedCurrentKey — a link exists, minted under the running key.
	// The only state in which the ledger says the link can still be withdrawn
	// here.
	EgressFeedIssuedCurrentKey EgressFeedState = "issued_current_key"
)

// EgressFeedStates enumerates the feed states in evaluation order.
var EgressFeedStates = []EgressFeedState{
	EgressFeedUnknown,
	EgressFeedNone,
	EgressFeedIssuedBeforeRecorded,
	EgressFeedIssuedPreviousKey,
	EgressFeedIssuedCurrentKey,
}

// EgressSectionState is the section heading's own state. It is a full automaton
// over the two paths rather than a summary of either, because the combination
// both paths quiet — an endpoint saved but switched off, no feed — is the
// ordinary configuration and belonged to neither path's states.
type EgressSectionState string

const (
	// EgressSectionNeedsAttention dominates: something is stored that cannot do
	// what it looks like it does.
	EgressSectionNeedsAttention EgressSectionState = "needs_attention"
	// EgressSectionPathsEnabled — at least one path can carry something out.
	EgressSectionPathsEnabled EgressSectionState = "paths_enabled"
	// EgressSectionNoPathEnabled — nothing leaves by either path.
	EgressSectionNoPathEnabled EgressSectionState = "no_path_enabled"
)

// EgressSectionStates enumerates the heading states in precedence order.
var EgressSectionStates = []EgressSectionState{
	EgressSectionNeedsAttention,
	EgressSectionPathsEnabled,
	EgressSectionNoPathEnabled,
}

// EgressPayloadField names one field a path carries off the instance. The
// ledger lists them because "nothing leaves" is a claim about CONTENT, and a
// surface that enumerates the pipes while staying silent about what runs through
// them answers a weaker question than the one an owner asked.
type EgressPayloadField string

// WebhookPayloadFields lists what a webhook reminder carries, by its wire name.
// It is pinned against WebhookPayload itself, so a field added to or removed
// from that struct fails the suite rather than quietly changing what leaves
// while the page keeps describing the old shape.
//
// event_date is a PREDICTED date and disclaimer is the medical-safety sentence:
// both are payload, not decoration, and are named here as things that leave.
func WebhookPayloadFields() []EgressPayloadField {
	return []EgressPayloadField{
		"title",
		"message",
		"disclaimer",
		"type",
		"event_date",
		"lead_days",
	}
}

// CalendarFeedPayloadFields lists the iCalendar properties one feed event
// carries. It is pinned against a rendered feed rather than against a struct,
// because the builder writes properties directly.
//
// uid is listed for the same reason event_date is listed above: it is built from
// the reminder kind and its date, so the kind leaves in it even though the
// summary is deliberately neutral.
func CalendarFeedPayloadFields() []EgressPayloadField {
	return []EgressPayloadField{
		"uid",
		"dtstamp",
		"dtstart",
		"dtend",
		"summary",
		"description",
		"transp",
	}
}

// EgressWebhookLedger is the webhook path's rendered account. The three stored
// toggles ride it rather than the page's unconditional view data: they are this
// owner's delivery configuration, and a session that is not the owner must
// receive their ABSENCE, not a false reading of them.
type EgressWebhookLedger struct {
	State           EgressWebhookState
	Enabled         bool
	NotifyPeriod    bool
	NotifyOvulation bool
	// Host is at most a hostname, and only in a state that opened the ciphertext.
	Host string
	// LastDeliveredAt is the ONLY delivery evidence, and it comes only from
	// webhook_last_delivered_at, written after a 2xx. It is suppressed in every
	// state that cannot vouch for the endpoint it would describe.
	LastDeliveredAt *time.Time
	PayloadFields   []EgressPayloadField
}

// EgressFeedLedger is the .ics path's rendered account.
type EgressFeedLedger struct {
	State EgressFeedState
	// RevealedAt marks the one-time reveal as CONSUMED. It is not a fetch, not a
	// display, and not a count: the server observes a claim being spent and
	// nothing about whether the link was ever opened.
	RevealedAt    *time.Time
	PayloadFields []EgressPayloadField
}

// EgressLedger is the whole owner-scoped account. It is built only for an owner
// and reaches the view layer only through a gated optional block, so a session
// that is not the row's owner receives no field of it at all — not a false one.
type EgressLedger struct {
	Section EgressSectionState
	Webhook EgressWebhookLedger
	Feed    EgressFeedLedger
}

// EgressLedgerWebhookDisplayBuilder is the readability seam. It exists so the
// ledger never holds the secret key and never sees a decrypt error's text.
type EgressLedgerWebhookDisplayBuilder interface {
	BuildWebhookURLDisplay(userID uint, encryptedURL string) WebhookURLDisplay
}

// EgressLedgerFeedStatusBuilder is the feed seam. It returns no token, no
// selector and no URL.
type EgressLedgerFeedStatusBuilder interface {
	BuildFeedStatus(ctx context.Context, userID uint) CalendarFeedStatus
}

// EgressLedgerService assembles the ledger. outboundDeliveryEnabled is a domain
// fact — "this instance runs a delivery pass" — resolved once at composition
// time from the runtime configuration, so no handler and no template ever reads
// configuration for itself.
type EgressLedgerService struct {
	webhook                 EgressLedgerWebhookDisplayBuilder
	feed                    EgressLedgerFeedStatusBuilder
	outboundDeliveryEnabled bool
}

// NewEgressLedgerService wires the ledger.
func NewEgressLedgerService(webhook EgressLedgerWebhookDisplayBuilder, feed EgressLedgerFeedStatusBuilder, outboundDeliveryEnabled bool) *EgressLedgerService {
	return &EgressLedgerService{webhook: webhook, feed: feed, outboundDeliveryEnabled: outboundDeliveryEnabled}
}

// BuildEgressLedger derives the ledger for one owner. user carries the webhook
// columns already loaded by the settings projection; the feed half is read
// through its own seam, which is where a load failure becomes "unknown".
func (service *EgressLedgerService) BuildEgressLedger(ctx context.Context, user models.User) EgressLedger {
	display := service.webhook.BuildWebhookURLDisplay(user.ID, user.WebhookURL)
	webhookState := service.resolveWebhookState(user, display)

	feedStatus := service.feed.BuildFeedStatus(ctx, user.ID)
	feedState := resolveFeedState(feedStatus)

	return EgressLedger{
		Section: resolveEgressSectionState(webhookState, feedState),
		Webhook: EgressWebhookLedger{
			State:           webhookState,
			Enabled:         user.WebhookEnabled,
			NotifyPeriod:    user.WebhookNotifyPeriod,
			NotifyOvulation: user.WebhookNotifyOvulation,
			Host:            webhookHostForState(webhookState, display),
			LastDeliveredAt: deliveryEvidenceForState(webhookState, user.WebhookLastDeliveredAt),
			PayloadFields:   WebhookPayloadFields(),
		},
		Feed: EgressFeedLedger{
			State:         feedState,
			RevealedAt:    revealEvidenceForState(feedState, feedStatus.RevealedAt),
			PayloadFields: CalendarFeedPayloadFields(),
		},
	}
}

// resolveWebhookState applies the one order the states may be evaluated in.
// Readability comes before every toggle, and the instance-wide fact comes before
// the per-owner ones: each earlier case describes a reason the later ones cannot
// matter.
func (service *EgressLedgerService) resolveWebhookState(user models.User, display WebhookURLDisplay) EgressWebhookState {
	switch {
	case display.Readability == WebhookURLAbsent:
		return EgressWebhookNotConfigured
	case display.Readability == WebhookURLUnreadable:
		return EgressWebhookUnreadable
	case strings.TrimSpace(display.Host) == "":
		return EgressWebhookUnusable
	case !service.outboundDeliveryEnabled:
		return EgressWebhookOutboundDisabled
	case !user.WebhookEnabled:
		return EgressWebhookStoredOff
	case !user.WebhookNotifyPeriod && !user.WebhookNotifyOvulation:
		return EgressWebhookStoredNoKinds
	default:
		return EgressWebhookArmed
	}
}

// resolveFeedState answers only what the recorded epoch permits. An instance
// that cannot derive its own epoch says "unknown" rather than reporting a
// mismatch it did not measure.
func resolveFeedState(status CalendarFeedStatus) EgressFeedState {
	switch {
	case !status.Known:
		return EgressFeedUnknown
	case !status.Configured:
		return EgressFeedNone
	case status.CurrentKeyEpoch == "":
		return EgressFeedUnknown
	case status.KeyEpoch == "":
		return EgressFeedIssuedBeforeRecorded
	case subtle.ConstantTimeCompare([]byte(status.KeyEpoch), []byte(status.CurrentKeyEpoch)) != 1:
		return EgressFeedIssuedPreviousKey
	default:
		return EgressFeedIssuedCurrentKey
	}
}

// resolveEgressSectionState is the heading automaton. needs_attention dominates
// so a working path never masks a broken one.
func resolveEgressSectionState(webhook EgressWebhookState, feed EgressFeedState) EgressSectionState {
	switch webhook {
	case EgressWebhookUnreadable, EgressWebhookUnusable:
		return EgressSectionNeedsAttention
	}
	switch feed {
	case EgressFeedUnknown, EgressFeedIssuedPreviousKey:
		return EgressSectionNeedsAttention
	}
	if webhook == EgressWebhookArmed {
		return EgressSectionPathsEnabled
	}
	switch feed {
	case EgressFeedIssuedCurrentKey, EgressFeedIssuedBeforeRecorded:
		return EgressSectionPathsEnabled
	}
	return EgressSectionNoPathEnabled
}

// webhookHostForState surfaces a host only where one was actually derived.
func webhookHostForState(state EgressWebhookState, display WebhookURLDisplay) string {
	if state == EgressWebhookNotConfigured || state == EgressWebhookUnreadable {
		return ""
	}
	return display.Host
}

// deliveryEvidenceForState suppresses the delivery mark in every state that
// cannot vouch for the endpoint the mark describes.
//
// The suppression is not cosmetic. The clearing rule fires on a SAVE, so a row
// whose ciphertext stopped opening — a rotated key, never re-saved — still holds
// the mark from the last delivery under the old key. Rendered unconditionally it
// would read "the last delivery was accepted on <date>" directly beneath "this
// instance can no longer open that endpoint".
func deliveryEvidenceForState(state EgressWebhookState, deliveredAt *time.Time) *time.Time {
	switch state {
	case EgressWebhookNotConfigured, EgressWebhookUnreadable, EgressWebhookUnusable, EgressWebhookOutboundDisabled:
		return nil
	}
	return deliveredAt
}

// revealEvidenceForState suppresses the reveal mark where there is no link for
// it to be about.
func revealEvidenceForState(state EgressFeedState, revealedAt *time.Time) *time.Time {
	if state == EgressFeedUnknown || state == EgressFeedNone {
		return nil
	}
	return revealedAt
}
