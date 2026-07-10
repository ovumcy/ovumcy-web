-- Postgres mirror of migrations/030_week_start.sql (week-start display
-- preference, issue #225). Same version number so schema history stays aligned
-- across engines.
--
-- week_starts_on holds the owner's first-day-of-week preference ('sunday' or
-- 'monday'), defaulting to 'sunday' so existing rows keep the current
-- Sunday-first calendar layout. A pure presentation preference -- not sensitive,
-- not a secret.
--
-- ALTER TABLE ADD COLUMN IF NOT EXISTS keeps the migration idempotent across the
-- postgres test bootstrap and rolling deploys (in addition to the runner's own
-- already-exists skip). Rollback (forward-only repo) is documented in the commit
-- body, not here.

ALTER TABLE users ADD COLUMN IF NOT EXISTS week_starts_on TEXT NOT NULL DEFAULT 'sunday';
