package models

import "time"

// AppStateKeyLastReminderRunDate is the app_state key under which the built-in
// reminder scheduler (issue #125) records the server-local date (YYYY-MM-DD) it
// last completed a pass. It backs restart safety (a same-day restart never
// re-fires) and current-day catch-up after downtime. It is the single source of
// truth for the key string so the scheduler and any tooling cannot drift.
const AppStateKeyLastReminderRunDate = "last_reminder_run_date"

// AppState is one row of the process-level key/value store (migration 028).
// It holds runtime bookkeeping, NEVER special-category health data, and is not
// scoped by user_id — it is deliberately outside the users table. Value is
// opaque TEXT written only by the single scheduler goroutine.
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
