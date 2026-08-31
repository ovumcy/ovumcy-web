-- Postgres mirror of migrations/038_webhook_config_version.sql (finding
-- PRIV-1 / SEC-01). Same version number and basename so schema history stays
-- aligned across engines.
--
-- webhook_config_version is the monotonic revocation epoch of an owner's
-- webhook configuration: every settings save, disable, remove and clear-data
-- wipe increments it in the same statement that performs the write, and the
-- pre-delivery watermark claim pins the value the claiming pass's snapshot
-- carried, so a pass that read the configuration before a revocation can no
-- longer win a claim and POST to the revoked endpoint. Clear-data ADVANCES the
-- counter rather than resetting it, because a reset would hand a revoked
-- snapshot its own value back.
--
-- ALTER TABLE ADD COLUMN IF NOT EXISTS keeps the migration idempotent across
-- the postgres test bootstrap and rolling deploys (in addition to the runner's
-- own already-exists skip). Rollback (forward-only repo) is documented in the
-- commit body, not here.

ALTER TABLE users ADD COLUMN IF NOT EXISTS webhook_config_version INTEGER NOT NULL DEFAULT 0;
