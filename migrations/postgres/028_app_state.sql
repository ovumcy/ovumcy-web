-- Postgres mirror of migrations/028_app_state.sql (issue #125: built-in reminder
-- scheduler). Same version number so schema history stays aligned across
-- engines.
--
-- A minimal key/value store for process-level runtime markers that are NOT
-- per-owner health data. Its only key so far is last_reminder_run_date, the
-- server-local date the in-process reminder scheduler last completed a pass
-- (restart safety + current-day catch-up). No special-category data, not scoped
-- by user_id. CREATE TABLE IF NOT EXISTS keeps it idempotent across the postgres
-- bootstrap and rolling deploys, in addition to the runner's own skip. Rollback
-- (forward-only repo) is documented in the commit body, not here.

CREATE TABLE IF NOT EXISTS app_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
