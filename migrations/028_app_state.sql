-- App-level key/value state (issue #125: built-in reminder scheduler).
--
-- A minimal, single-row-per-key store for process-level runtime markers that are
-- NOT per-owner health data. Its first and only key so far is
-- last_reminder_run_date=YYYY-MM-DD (the server-local date the in-process
-- reminder scheduler last completed a pass), used for restart safety and
-- current-day catch-up after downtime.
--
-- This table holds NO special-category health data and is NOT scoped by user_id:
-- it is process/runtime bookkeeping, deliberately kept out of the users table so
-- the settings whitelist stays the single source of per-owner columns. The value
-- column is opaque TEXT written only by the single scheduler goroutine.
--
-- Idempotency: the scheduler writes via UPSERT (see the postgres mirror's ON
-- CONFLICT and the repository's clause.OnConflict), so re-running a pass just
-- overwrites the date. The migration itself is idempotent through
-- CREATE TABLE IF NOT EXISTS and the runner's already-applied skip. Rollback
-- (forward-only repo) is documented in the commit body, not here.

CREATE TABLE IF NOT EXISTS app_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
