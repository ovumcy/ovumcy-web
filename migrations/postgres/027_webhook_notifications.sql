-- Postgres mirror of migrations/027_webhook_notifications.sql (issue #124,
-- slice 1: data plumbing only). Same version number so schema history stays
-- aligned across engines.
--
-- ALTER TABLE ADD COLUMN IF NOT EXISTS keeps the migration idempotent across
-- the postgres test bootstrap and rolling deploys (in addition to the runner's
-- own already-exists skip). webhook_url stores CIPHERTEXT only (aad-bound to
-- users.id), never the plaintext URL. reminder_lead_days is the SHARED banner
-- plus webhook lead window, default 3, matching the banner fallback constant.
-- Rollback (forward-only repo) is documented in the commit body, not here.

ALTER TABLE users ADD COLUMN IF NOT EXISTS webhook_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS webhook_url TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS webhook_notify_period BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS webhook_notify_ovulation BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS webhook_period_last_sent_cycle_start DATE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS webhook_ovulation_last_sent_cycle_start DATE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS reminder_lead_days INTEGER NOT NULL DEFAULT 3;
