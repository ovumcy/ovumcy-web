package models

import "time"

const (
	RoleOwner           = "owner"
	DefaultCycleLength  = 28
	DefaultPeriodLength = 5
	// DefaultReminderLeadDays is the column default for users.reminder_lead_days
	// (issue #124) — the SHARED banner + webhook lead window. It matches
	// services.DashboardReminderBannerWindowDays; kept in models so the db
	// clear-data reset can reference it without importing services (layering).
	DefaultReminderLeadDays = 3
	// Age brackets are calibrated to the medical literature: 35–39 is the
	// lowest-variability cohort in Gibson et al., npj Digital Medicine 2023
	// (Apple Women's Health Study, n=12,608), with within-individual cycle SD
	// rising only modestly through 40–44 and sharply at 45+. Persistent ≥7-day
	// differences between consecutive cycles after 40 are the STRAW+10 marker
	// for the menopausal transition (median entry age 45.5 years), so the
	// three brackets isolate the clinically meaningful threshold at 45.
	AgeGroupUnknown = ""
	AgeGroupUnder40 = "under_40"
	AgeGroup40To45  = "age_40_45"
	AgeGroup45Plus  = "age_45_plus"
	UsageGoalHealth = "health"
	UsageGoalAvoid  = "avoid_pregnancy"
	UsageGoalTrying = "trying_to_conceive"
	// Week-start display preference (issue #225): the first day of the week for
	// the calendar grid/header. Kept in models so the migration default, the
	// gorm column default, the db clear-data reset, and services normalization
	// all reference one source of truth without a layering violation. Default is
	// Sunday to preserve the pre-#225 layout for existing owners.
	WeekStartSunday  = "sunday"
	WeekStartMonday  = "monday"
	DefaultWeekStart = WeekStartSunday
)

type User struct {
	ID                   uint       `gorm:"primaryKey"`
	DisplayName          string     `gorm:"size:80"`
	Email                string     `gorm:"uniqueIndex;not null"`
	PasswordHash         string     `gorm:"not null"`
	RecoveryCodeHash     string     `gorm:"column:recovery_code_hash"`
	LocalAuthEnabled     bool       `gorm:"column:local_auth_enabled;not null"`
	AuthSessionVersion   int        `gorm:"column:auth_session_version;not null;default:1"`
	MustChangePassword   bool       `gorm:"column:must_change_password;not null;default:false"`
	Role                 string     `gorm:"not null;default:owner"`
	OnboardingCompleted  bool       `gorm:"not null;default:false"`
	CycleLength          int        `gorm:"not null;default:28"`
	PeriodLength         int        `gorm:"not null;default:5"`
	LutealPhase          int        `gorm:"column:luteal_phase;not null;default:14"`
	AutoPeriodFill       bool       `gorm:"column:auto_period_fill;not null;default:true"`
	IrregularCycle       bool       `gorm:"column:irregular_cycle;not null;default:false"`
	TrackBBT             bool       `gorm:"column:track_bbt;not null;default:false"`
	TemperatureUnit      string     `gorm:"column:temperature_unit;not null;default:c"`
	TrackCervicalMucus   bool       `gorm:"column:track_cervical_mucus;not null;default:false"`
	HideSexChip          bool       `gorm:"column:hide_sex_chip;not null;default:false"`
	HideCycleFactors     bool       `gorm:"column:hide_cycle_factors;not null;default:false"`
	HideNotesField       bool       `gorm:"column:hide_notes_field;not null;default:false"`
	ShowHistoricalPhases bool       `gorm:"column:show_historical_phases;not null;default:false"`
	WeekStartsOn         string     `gorm:"column:week_starts_on;not null;default:sunday"`
	ShownPeriodTip       bool       `gorm:"column:shown_period_tip;not null;default:false"`
	AgeGroup             string     `gorm:"column:age_group;not null;default:''"`
	UsageGoal            string     `gorm:"column:usage_goal;not null;default:health"`
	UnpredictableCycle   bool       `gorm:"column:unpredictable_cycle;not null;default:false"`
	LongPeriodWarnedAt   *time.Time `gorm:"column:long_period_warning_cycle_start;type:date"`
	LastPeriodStart      *time.Time `gorm:"type:date"`
	CreatedAt            time.Time  `gorm:"not null"`
	TOTPSecret           string     `gorm:"column:totp_secret"`
	TOTPEnabled          bool       `gorm:"column:totp_enabled;not null;default:false"`
	TOTPLastUsedStep     int64      `gorm:"column:totp_last_used_step;not null;default:0"`
	// Timezone is the owner's last known IANA timezone name (e.g.
	// "Europe/Belgrade"), persisted from the request so request-free batch
	// passes (webhook reminders, issue #124) can resolve "today" without a
	// browser. Nullable/empty when never observed; only validated IANA values
	// are written (see api.parseRequestTimezone). Not sensitive, not a secret.
	Timezone string `gorm:"column:timezone"`
	// Webhook notification settings (issue #124). A future request-free batch
	// pass reads these to decide whether to POST a period/ovulation reminder to
	// an owner-configured webhook. This block is storage only — no reminder
	// decision, delivery, or CLI lives here (later slices).
	//
	// WebhookEnabled is the master switch for outbound webhook reminders.
	WebhookEnabled bool `gorm:"column:webhook_enabled;not null;default:false"`
	// WebhookURL holds CIPHERTEXT, never the plaintext endpoint — encrypted at
	// rest via security.EncryptField and aad-bound to this user's id
	// ("ovumcy.field.webhook_url:<id>"), exactly like TOTPSecret. Nullable/empty
	// when no webhook is configured. Never expose this raw in transport or logs.
	WebhookURL string `gorm:"column:webhook_url"`
	// WebhookNotifyPeriod / WebhookNotifyOvulation are the per-kind opt-ins;
	// both default true so enabling the webhook sends both reminder kinds unless
	// the owner narrows it.
	WebhookNotifyPeriod    bool `gorm:"column:webhook_notify_period;not null;default:true"`
	WebhookNotifyOvulation bool `gorm:"column:webhook_notify_ovulation;not null;default:true"`
	// WebhookPeriodLastSentCycleStart / WebhookOvulationLastSentCycleStart are
	// watermarks storing the cycle-start anchor a reminder of each kind was last
	// sent for, so the future notify pass sends at most one reminder per cycle.
	// Nil until the first send. Stored as UTC-midnight DATE like LastPeriodStart.
	WebhookPeriodLastSentCycleStart    *time.Time `gorm:"column:webhook_period_last_sent_cycle_start;type:date"`
	WebhookOvulationLastSentCycleStart *time.Time `gorm:"column:webhook_ovulation_last_sent_cycle_start;type:date"`
	// ReminderLeadDays is the SHARED lead window (in days) for BOTH the in-app
	// dashboard banner (issue #123) and webhook reminders: a reminder surfaces
	// once the predicted event is within this many days of "today". Default 3
	// matches services.DashboardReminderBannerWindowDays; bounded 0–14 at save.
	ReminderLeadDays int `gorm:"column:reminder_lead_days;not null;default:3"`
	// Calendar (.ics) feed subscription token (slice 1: storage only). Backs a
	// pull-based feed URL whose path carries a bearer capability token; a
	// calendar client polls it for the owner's own cycle events. Both columns are
	// empty when the feed is off (the default zero value). This block is storage
	// only — no endpoint, .ics builder, rate-limit, or settings UI lives here
	// (later slices).
	//
	// The token is split SELECTOR + VERIFIER so a feed request (which carries no
	// email) resolves the row with one indexed lookup instead of an O(N) bcrypt
	// scan over every user.
	//
	// CalendarFeedSelector is the NON-secret lookup id: high-entropy but it only
	// NAMES the row, so it is stored in plaintext. A PARTIAL unique index (on
	// non-empty values, migration 029) enforces cross-owner uniqueness for armed
	// feeds while letting every feed-off owner share the empty-string zero value.
	// It is not a credential on its own.
	CalendarFeedSelector string `gorm:"column:calendar_feed_selector"`
	// CalendarFeedVerifierHash holds the BCRYPT hash of the secret verifier half,
	// never the verifier plaintext. The full token (selector+verifier) is shown
	// to the owner exactly once at generation and is not retrievable afterward,
	// mirroring the recovery-code shown-once model. Verification looks the row up
	// by selector, then constant-time bcrypt-compares the verifier; a missing
	// selector and a wrong verifier both resolve to the same "not found".
	CalendarFeedVerifierHash string `gorm:"column:calendar_feed_verifier_hash"`
}

// CalendarFeedTokenColumns is the transport-free narrow payload written by the
// calendar-feed token write path (slice 1). Selector is the NON-secret,
// UNIQUE-indexed lookup id; VerifierHash is already a BCRYPT hash — the service
// hashes the secret verifier before building this struct, so persistence never
// sees the verifier plaintext. It carries only the two feed-token columns and
// no security-posture field: writing it must not bump auth_session_version.
type CalendarFeedTokenColumns struct {
	Selector     string
	VerifierHash string
}
