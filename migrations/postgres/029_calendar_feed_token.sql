-- Postgres mirror of migrations/029_calendar_feed_token.sql (calendar .ics feed
-- subscription token, slice 1: token storage only). Same version number so
-- schema history stays aligned across engines.
--
-- calendar_feed_selector is the NON-secret lookup id, stored in plaintext and
-- indexed for the by-selector feed lookup. calendar_feed_verifier_hash stores
-- the bcrypt hash of the SECRET verifier half. The verifier plaintext is NEVER
-- stored and the full token is shown to the owner exactly once at generation.
--
-- The model persists an empty string for a fresh/off feed (a Go string zero
-- value), so the UNIQUE index is PARTIAL -- it covers only rows whose selector
-- is non-empty. Without the predicate every feed-off row would share the
-- empty-string key and collide on the second insert. An armed feed's selector is
-- unique across owners so the by-selector lookup resolves exactly one row.
--
-- ALTER TABLE ADD COLUMN IF NOT EXISTS keeps the migration idempotent across the
-- postgres test bootstrap and rolling deploys (in addition to the runner's own
-- already-exists skip). Rollback (forward-only repo) is documented in the commit
-- body, not here.
--
-- NOTE: keep prose in this file free of semicolons -- the migration runner
-- splits statements on the semicolon character without stripping SQL comments.

ALTER TABLE users ADD COLUMN IF NOT EXISTS calendar_feed_selector TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS calendar_feed_verifier_hash TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_calendar_feed_selector
    ON users (calendar_feed_selector)
    WHERE calendar_feed_selector <> '';
