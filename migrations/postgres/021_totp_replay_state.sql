-- Postgres mirror of migrations/021_totp_replay_state.sql. ALTER TABLE …
-- ADD COLUMN IF NOT EXISTS keeps the migration idempotent across the
-- postgres test bootstrap and rolling deploys.
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_last_used_step BIGINT NOT NULL DEFAULT 0;
