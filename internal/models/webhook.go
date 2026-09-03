package models

import "time"

const (
	// WebhookReminderTypePeriod and WebhookReminderTypeOvulation identify which
	// upcoming prediction a webhook reminder summarizes. They are the single
	// source of truth for the reminder-kind strings shared across layers: the
	// services decision (DueReminderType*) aliases them, and the db watermark
	// write maps them to their per-kind column — so the wire value, the decision
	// value, and the persisted watermark key can never drift.
	WebhookReminderTypePeriod    = "period-soon"
	WebhookReminderTypeOvulation = "ovulation-soon"
)

// WebhookSettingsColumns is the transport-free narrow payload written by the
// webhook-settings save path (issue #124). EncryptedURL is already CIPHERTEXT —
// the service encrypts the plaintext endpoint before building this struct, so
// persistence never sees a plaintext URL. It carries only the settings columns,
// never a security-posture field: writing it must not bump auth_session_version.
type WebhookSettingsColumns struct {
	Enabled          bool
	EncryptedURL     string
	NotifyPeriod     bool
	NotifyOvulation  bool
	ReminderLeadDays int
	// ClearLastDeliveredAt asks the same UPDATE that writes EncryptedURL to NULL
	// webhook_last_delivered_at (migration 039). It is the caller's judgement
	// because only the caller can make it: persistence stores ciphertext and
	// cannot tell a re-encryption of the same endpoint from a different one. The
	// service sets it whenever it cannot PROVE the destination is unchanged — a
	// replaced URL, a removed one, or a stored ciphertext that no longer opens.
	//
	// False, the zero value, means "leave the mark standing", which is what a
	// toggle-only save wants. A caller that forgets the field therefore keeps a
	// mark that may now be about a different endpoint, and that is why the rule is
	// pinned by enumerating the writers of webhook_url rather than by this
	// comment: TestEveryWebhookURLWriterDecidesTheDeliveryMark.
	ClearLastDeliveredAt bool
	// KeepEncryptedURL asks the UPDATE to leave webhook_url alone entirely. It
	// exists for one situation: the stored ciphertext no longer opens under this
	// instance's key, so there is no plaintext to re-encrypt and no honest value
	// to write. Substituting the empty string there DELETED an endpoint the
	// request never asked to remove, and refusing the whole save instead made
	// every unrelated toggle on the same form unusable until the endpoint was
	// dealt with. Keeping the column is the third answer: the toggles persist,
	// the endpoint stays for a deliberate withdrawal, and delivery cannot be
	// armed over it because the service refuses that separately.
	//
	// It is mutually exclusive with EncryptedURL by construction: a save that
	// keeps the column has nothing to put in it. Regression:
	// TestSaveWebhookSettingsKeepsAnUnreadableEndpointAndStillSavesTheToggles.
	KeepEncryptedURL bool
}

// WebhookNotifyRecord is the read projection returned by ListAllForNotify: the
// exact columns a future request-free batch pass needs to decide and send
// webhook reminders, and nothing else. EncryptedURL is CIPHERTEXT (decrypt via
// WebhookSettingsService.DecryptWebhookURL, aad-bound to ID). The two
// *LastSentCycleStart watermarks gate at most one reminder of each kind per
// cycle. Timezone lets the pass resolve "today" without a browser request, and
// InterfaceLanguage lets it render the payload in the language the owner chose
// — both are the request-free pass's substitute for a browser it never sees.
//
// It is intentionally NOT models.User: LoadSettingsByID stays the single
// settings whitelist, and this projection is scoped to the notify use case so
// the batch query never over-selects sensitive per-account columns.
type WebhookNotifyRecord struct {
	ID uint `gorm:"column:id"`

	// Cycle prediction inputs.
	CycleLength        int        `gorm:"column:cycle_length"`
	PeriodLength       int        `gorm:"column:period_length"`
	LutealPhase        int        `gorm:"column:luteal_phase"`
	IrregularCycle     bool       `gorm:"column:irregular_cycle"`
	UnpredictableCycle bool       `gorm:"column:unpredictable_cycle"`
	LastPeriodStart    *time.Time `gorm:"column:last_period_start;type:date"`
	Timezone           string     `gorm:"column:timezone"`

	// InterfaceLanguage is the UI language the owner chose (users.interface_language,
	// empty when they never chose one). It is the durable carrier of that choice,
	// so the notify pass resolves every localized payload field at it instead of
	// at the server default. Not a credential and not a health specific: it names
	// a language, nothing about the account.
	InterfaceLanguage string `gorm:"column:interface_language"`

	// Webhook settings.
	WebhookEnabled         bool   `gorm:"column:webhook_enabled"`
	WebhookURL             string `gorm:"column:webhook_url"`
	WebhookNotifyPeriod    bool   `gorm:"column:webhook_notify_period"`
	WebhookNotifyOvulation bool   `gorm:"column:webhook_notify_ovulation"`
	ReminderLeadDays       int    `gorm:"column:reminder_lead_days"`

	// WebhookConfigVersion is the revocation epoch of the settings above at the
	// instant this snapshot was read. The pass hands it back to
	// ClaimWebhookWatermark, which refuses the claim when the stored value has
	// moved on — so a snapshot taken before the owner disabled delivery, replaced
	// or removed the endpoint, or cleared their data can no longer reach that
	// endpoint. It is a coordination value, not a setting: nothing in the pass
	// decides anything from it except whether its own claim still applies.
	WebhookConfigVersion int `gorm:"column:webhook_config_version"`

	// Per-kind watermarks (cycle-start anchor a reminder was last sent for).
	WebhookPeriodLastSentCycleStart    *time.Time `gorm:"column:webhook_period_last_sent_cycle_start;type:date"`
	WebhookOvulationLastSentCycleStart *time.Time `gorm:"column:webhook_ovulation_last_sent_cycle_start;type:date"`
}
