-- Postgres mirror of migrations/034_user_interface_language.sql (interface
-- language as an account property). Same version number so schema history stays
-- aligned across engines.
--
-- interface_language holds the UI language the owner chose explicitly in
-- Settings. The empty string default means "never chosen", in which case the
-- request resolves the language exactly as before (cookie, Accept-Language,
-- DEFAULT_LANGUAGE). A pure presentation preference -- not health data, not a
-- secret.
--
-- Note for a later editor: the runner splits a file into statements on the
-- semicolon, so a comment block must not contain one.
--
-- ALTER TABLE ADD COLUMN IF NOT EXISTS keeps the migration idempotent across the
-- postgres test bootstrap and rolling deploys (in addition to the runner's own
-- already-exists skip). Rollback (forward-only repo) is documented in the commit
-- body, not here.

ALTER TABLE users ADD COLUMN IF NOT EXISTS interface_language TEXT NOT NULL DEFAULT '';
