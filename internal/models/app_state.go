package models

import "time"

// AppStateKeyLastReminderRunDate is the app_state key under which the built-in
// reminder scheduler (issue #125) records the server-local date (YYYY-MM-DD) it
// last completed a pass. It backs restart safety (a same-day restart never
// re-fires) and current-day catch-up after downtime. It is the single source of
// truth for the key string so the scheduler and any tooling cannot drift.
const AppStateKeyLastReminderRunDate = "last_reminder_run_date"

// AppStateKeyCalendarFeedKeyEpoch is the app_state key under which the
// boot-time key-rotation sentinel records the current calendar-feed key epoch
// (an irreversible value derived from SECRET_KEY — see
// security.CalendarFeedKeyEpoch). A mismatch on boot means the key was rotated
// (or the feed-MAC labels were bumped) since the last start, and the sentinel
// disarms every legacy pre-032 feed row that would otherwise keep verifying
// through its key-independent bcrypt hash.
const AppStateKeyCalendarFeedKeyEpoch = "calendar_feed_key_epoch"

// AppState is one row of the process-level key/value store (migration 028).
// It holds runtime bookkeeping, NEVER special-category health data, and is not
// scoped by user_id — it is deliberately outside the users table. Value is
// opaque TEXT with a single writer per key: the scheduler goroutine owns
// last_reminder_run_date, and the boot-time key-rotation sentinel writes
// calendar_feed_key_epoch before the server starts serving (never concurrently
// with it).
type AppState struct {
	Key       string    `gorm:"column:key;primaryKey"`
	Value     string    `gorm:"column:value;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName pins the table to app_state (GORM would otherwise pluralize to
// app_states).
func (AppState) TableName() string {
	return "app_state"
}
