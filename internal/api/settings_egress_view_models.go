package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Transport-side projection of the egress ledger.
//
// Every i18n key below is a LITERAL at its own call site. The natural shape here
// is fmt.Sprintf("settings.egress.state.webhook.%s", state), and it is refused:
// the reachability barrier resolves computed key families only through a
// registered enumerator, so an assembled key would either fail the run by name
// or, once registered, put the catalogue's reachability behind a second
// declaration that can drift from this switch. A literal per state keeps the
// sweep able to see every key without knowing anything about this file.

// settingsEgressPathView is one path's rendered row. The timestamp is carried as
// two separate strings — a machine-readable attribute value and a display string
// — so no translated sentence ever has to interpolate a date.
type settingsEgressPathView struct {
	State        string
	StateKey     string
	Host         string
	EvidenceKey  string
	EvidenceISO  string
	EvidenceText string
	PayloadKeys  []string
}

// settingsEgressView is the whole owner-only block.
type settingsEgressView struct {
	Section    string
	SectionKey string

	Webhook settingsEgressPathView
	// WebhookRemovable is false only when there is no endpoint to withdraw.
	WebhookRemovable bool
	Enabled          bool
	NotifyPeriod     bool
	NotifyOvulation  bool

	Feed settingsEgressPathView
	// FeedActionable is false while the row could not be read: offering "create a
	// link" there is how an owner ends up with a second link beside one that
	// still works.
	FeedActionable bool
	// FeedTokenPresent chooses rotate+withdraw over create.
	FeedTokenPresent bool
}

// buildSettingsEgressView projects the domain ledger onto the template's view.
func buildSettingsEgressView(c fiber.Ctx, ledger services.EgressLedger) settingsEgressView {
	language := currentLanguage(c)

	webhookEvidenceKey := "settings.egress.evidence.webhook.none"
	if ledger.Webhook.LastDeliveredAt != nil {
		webhookEvidenceKey = "settings.egress.evidence.webhook.recorded"
	}
	feedEvidenceKey := "settings.egress.evidence.feed.none"
	if ledger.Feed.RevealedAt != nil {
		feedEvidenceKey = "settings.egress.evidence.feed.recorded"
	}

	webhookISO, webhookText := egressTimestampStrings(language, ledger.Webhook.LastDeliveredAt)
	feedISO, feedText := egressTimestampStrings(language, ledger.Feed.RevealedAt)

	return settingsEgressView{
		Section:    string(ledger.Section),
		SectionKey: egressSectionStateMessageKey(ledger.Section),

		Webhook: settingsEgressPathView{
			State:        string(ledger.Webhook.State),
			StateKey:     egressWebhookStateMessageKey(ledger.Webhook.State),
			Host:         ledger.Webhook.Host,
			EvidenceKey:  webhookEvidenceKey,
			EvidenceISO:  webhookISO,
			EvidenceText: webhookText,
			PayloadKeys:  egressWebhookPayloadMessageKeys(ledger.Webhook.PayloadFields),
		},
		WebhookRemovable: ledger.Webhook.State != services.EgressWebhookNotConfigured,
		Enabled:          ledger.Webhook.Enabled,
		NotifyPeriod:     ledger.Webhook.NotifyPeriod,
		NotifyOvulation:  ledger.Webhook.NotifyOvulation,

		Feed: settingsEgressPathView{
			State:        string(ledger.Feed.State),
			StateKey:     egressFeedStateMessageKey(ledger.Feed.State),
			EvidenceKey:  feedEvidenceKey,
			EvidenceISO:  feedISO,
			EvidenceText: feedText,
			PayloadKeys:  egressFeedPayloadMessageKeys(ledger.Feed.PayloadFields),
		},
		FeedActionable:   ledger.Feed.State != services.EgressFeedUnknown,
		FeedTokenPresent: egressFeedTokenPresent(ledger.Feed.State),
	}
}

// egressTimestampStrings renders the one timestamp a path can prove, in the two
// forms the markup needs. The value never enters a translated sentence.
func egressTimestampStrings(language string, value *time.Time) (string, string) {
	if value == nil {
		return "", ""
	}
	stamp := value.UTC()
	return stamp.Format(time.RFC3339), services.LocalizedDateDisplay(language, stamp)
}

// egressFeedTokenPresent reports whether a link exists to rotate or withdraw.
func egressFeedTokenPresent(state services.EgressFeedState) bool {
	switch state {
	case services.EgressFeedIssuedCurrentKey, services.EgressFeedIssuedPreviousKey, services.EgressFeedIssuedBeforeRecorded:
		return true
	}
	return false
}

// egressSectionStateMessageKey names the heading sentence for each heading state.
func egressSectionStateMessageKey(state services.EgressSectionState) string {
	switch state {
	case services.EgressSectionNeedsAttention:
		return "settings.egress.state.section.needs_attention"
	case services.EgressSectionPathsEnabled:
		return "settings.egress.state.section.paths_enabled"
	case services.EgressSectionNoPathEnabled:
		return "settings.egress.state.section.no_path_enabled"
	}
	return ""
}

// egressWebhookStateMessageKey names the sentence for each webhook state.
func egressWebhookStateMessageKey(state services.EgressWebhookState) string {
	switch state {
	case services.EgressWebhookNotConfigured:
		return "settings.egress.state.webhook.not_configured"
	case services.EgressWebhookUnreadable:
		return "settings.egress.state.webhook.unreadable"
	case services.EgressWebhookUnusable:
		return "settings.egress.state.webhook.unusable"
	case services.EgressWebhookOutboundDisabled:
		return "settings.egress.state.webhook.outbound_disabled"
	case services.EgressWebhookStoredOff:
		return "settings.egress.state.webhook.stored_off"
	case services.EgressWebhookStoredNoKinds:
		return "settings.egress.state.webhook.stored_no_kinds"
	case services.EgressWebhookArmed:
		return "settings.egress.state.webhook.armed"
	}
	return ""
}

// egressFeedStateMessageKey names the sentence for each feed state.
func egressFeedStateMessageKey(state services.EgressFeedState) string {
	switch state {
	case services.EgressFeedUnknown:
		return "settings.egress.state.feed.unknown"
	case services.EgressFeedNone:
		return "settings.egress.state.feed.none"
	case services.EgressFeedIssuedBeforeRecorded:
		return "settings.egress.state.feed.issued_before_recorded"
	case services.EgressFeedIssuedPreviousKey:
		return "settings.egress.state.feed.issued_previous_key"
	case services.EgressFeedIssuedCurrentKey:
		return "settings.egress.state.feed.issued_current_key"
	}
	return ""
}

// egressWebhookPayloadMessageKeys names each field a reminder carries.
func egressWebhookPayloadMessageKeys(fields []services.EgressPayloadField) []string {
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case "title":
			keys = append(keys, "settings.egress.payload.webhook.title")
		case "message":
			keys = append(keys, "settings.egress.payload.webhook.message")
		case "disclaimer":
			keys = append(keys, "settings.egress.payload.webhook.disclaimer")
		case "type":
			keys = append(keys, "settings.egress.payload.webhook.type")
		case "event_date":
			keys = append(keys, "settings.egress.payload.webhook.event_date")
		case "lead_days":
			keys = append(keys, "settings.egress.payload.webhook.lead_days")
		}
	}
	return keys
}

// egressFeedPayloadMessageKeys names each iCalendar property a feed event carries.
func egressFeedPayloadMessageKeys(fields []services.EgressPayloadField) []string {
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case "uid":
			keys = append(keys, "settings.egress.payload.feed.uid")
		case "dtstamp":
			keys = append(keys, "settings.egress.payload.feed.dtstamp")
		case "dtstart":
			keys = append(keys, "settings.egress.payload.feed.dtstart")
		case "dtend":
			keys = append(keys, "settings.egress.payload.feed.dtend")
		case "summary":
			keys = append(keys, "settings.egress.payload.feed.summary")
		case "description":
			keys = append(keys, "settings.egress.payload.feed.description")
		case "transp":
			keys = append(keys, "settings.egress.payload.feed.transp")
		}
	}
	return keys
}
