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

// AppStateKeyCalendarFeedRestoreFence is the app_state key under which the
// boot-time restore fence records the token identifying the run of this
// instance that last wrote this database. Its twin lives OUTSIDE the database
// (security.CalendarFeedFenceFile, an operator-mounted path no database backup
// carries), which is the whole point: the key-epoch value above is restored
// together with the feed columns, so it can never disagree with them, while
// this one survives the restore and disagrees with the rolled-back copy. A
// mismatch means the database is not the one this instance last ran with — a
// restore, or a recreated fence — and every armed feed is disarmed, MAC or not,
// because SECRET_KEY did not change and each of them would still verify.
const AppStateKeyCalendarFeedRestoreFence = "calendar_feed_restore_fence"

// AppStateKeyCalendarFeedFenceUnanchored records that this database was booted
// at least once by a server that had NO usable fence above — path unset, no
// mount behind it, unreadable or unwritable. Only the PRESENCE of the key is
// read; the value is a human-readable note for an operator who finds the row.
//
// It exists because the two halves above cannot tell an installation that has
// never had a fence from one whose backup was taken while it had none: both
// halves are empty in either case, and calling that a first boot lets a feed
// the owner revoked on an unfenced instance come back through a restore, once
// the fence is finally mounted. A database carrying this key has run without
// the one marker a restore cannot roll back, so continuity across that gap is
// unprovable by construction and the first fenced boot disarms instead of
// adopting the rows in front of it.
//
// Written on every unanchored boot, erased by the boot that records a fresh
// token in both halves — the same pass that answers for it.
const AppStateKeyCalendarFeedFenceUnanchored = "calendar_feed_fence_unanchored"

// AppStateKeyAuthEmailRenormalizeV1 marks the one-shot boot pass that rewrote
// auth emails stored by the pre-strict normalizer (which kept a whole
// display-name-decorated input verbatim) down to the bare parsed address, so
// those accounts keep signing in under the strict addr-spec rule. Written once
// after the pass completes; its presence makes every later boot skip the scan.
const AppStateKeyAuthEmailRenormalizeV1 = "auth_email_renormalize.v1"

// AppStateKeyLutealPhaseRecomputeV1 marks the one-shot boot pass that recomputed
// the derived users.luteal_phase cache after the personalized luteal inference
// was corrected: rows written under the old convention hold a value one day too
// long, and an account whose logs no longer support an inference would keep
// predicting ovulation a day early forever. Written once after a pass in which
// no row failed; its presence makes every later boot skip the scan, and deleting
// the row forces one more pass.
const AppStateKeyLutealPhaseRecomputeV1 = "luteal_phase_recompute.v1"

// AppState is one row of the process-level key/value store (migration 028).
// It holds runtime bookkeeping, NEVER special-category health data, and is not
// scoped by user_id — it is deliberately outside the users table. Value is
// opaque TEXT with a single writer per key. Most of those writers run before
// the server serves: the key-rotation sentinel (calendar_feed_key_epoch), the
// one-shot email renormalizer (auth_email_renormalize.v1) and the one-shot
// luteal-phase recompute (luteal_phase_recompute.v1) all record their marker
// after their boot pass and never again. The scheduler goroutine owns
// last_reminder_run_date. calendar_feed_restore_fence is the one key written
// WHILE SERVING — every write that arms, rotates or removes a calendar feed
// advances it, which is what lets a restore be seen at all — so it is a single
// writer but not a boot marker, and the fence serializes its own writes.
// calendar_feed_fence_unanchored is that same fence's boot-time marker, and the
// one key any writer DELETES: the boot pass writes it whenever it ran without a
// usable fence and erases it on the boot that records a fresh token instead.
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
