package models

import "time"

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
}

// WebhookNotifyRecord is the read projection returned by ListAllForNotify: the
// exact columns a future request-free batch pass needs to decide and send
// webhook reminders, and nothing else. EncryptedURL is CIPHERTEXT (decrypt via
// WebhookSettingsService.DecryptWebhookURL, aad-bound to ID). The two
// *LastSentCycleStart watermarks gate at most one reminder of each kind per
// cycle. Timezone lets the pass resolve "today" without a browser request.
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

	// Webhook settings.
	WebhookEnabled         bool   `gorm:"column:webhook_enabled"`
	WebhookURL             string `gorm:"column:webhook_url"`
	WebhookNotifyPeriod    bool   `gorm:"column:webhook_notify_period"`
	WebhookNotifyOvulation bool   `gorm:"column:webhook_notify_ovulation"`
	ReminderLeadDays       int    `gorm:"column:reminder_lead_days"`

	// Per-kind watermarks (cycle-start anchor a reminder was last sent for).
	WebhookPeriodLastSentCycleStart    *time.Time `gorm:"column:webhook_period_last_sent_cycle_start;type:date"`
	WebhookOvulationLastSentCycleStart *time.Time `gorm:"column:webhook_ovulation_last_sent_cycle_start;type:date"`
}
