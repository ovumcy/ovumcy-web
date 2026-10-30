-- Postgres mirror of migrations/043_calendar_feed_detailed_titles.sql (WEB-379).
-- Same version number and basename so schema history stays aligned across
-- engines.
--
-- FALSE (the default, and every existing row) keeps the fixed neutral SUMMARY.
-- Written only by SaveCalendarFeedToken, in the same UPDATE as the token it
-- describes. A rollback to a binary without this column renders every feed
-- neutral.
--
-- ALTER TABLE ADD COLUMN IF NOT EXISTS keeps the migration idempotent across
-- the postgres test bootstrap and rolling deploys. Rollback (forward-only repo)
-- is documented in the commit body, not here.
--
-- NOTE: keep prose in this file free of semicolons -- the migration runner
-- splits statements on the semicolon character without stripping SQL comments.

ALTER TABLE users ADD COLUMN IF NOT EXISTS calendar_feed_detailed_titles BOOLEAN NOT NULL DEFAULT FALSE;
