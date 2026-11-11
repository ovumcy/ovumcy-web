-- Postgres mirror of migrations/045_calendar_feed_prediction_spans.sql
-- (WEB-381). Same version number and basename so schema history stays aligned
-- across engines.
--
-- FALSE (the default, and every existing row) keeps the feed body exactly as
-- before. Written only by SaveCalendarFeedToken, in the same UPDATE as the
-- token it describes, and reset by every disarm. A rollback to a binary
-- without these columns sends single estimated days.
--
-- ALTER TABLE ADD COLUMN IF NOT EXISTS keeps the migration idempotent across
-- the postgres test bootstrap and rolling deploys. Rollback (forward-only repo)
-- is documented in the commit body, not here.
--
-- NOTE: keep prose in this file free of semicolons -- the migration runner
-- splits statements on the semicolon character without stripping SQL comments.

ALTER TABLE users ADD COLUMN IF NOT EXISTS calendar_feed_period_span BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS calendar_feed_fertile_window BOOLEAN NOT NULL DEFAULT FALSE;
