-- Webhook notification settings (issue #124, slice 1: data plumbing only).
--
-- Adds the per-owner columns a future request-free batch pass will read to
-- decide whether to POST a period/ovulation reminder to an owner-configured
-- webhook. Storage ONLY in this slice: no reminder-decision logic, no outbound
-- delivery, no CLI.
--
-- Sensitive-at-rest: webhook_url stores CIPHERTEXT produced by
-- security.EncryptField (AES-256-GCM, aad-bound to users.id via
-- "ovumcy.field.webhook_url:<id>"), never the plaintext URL, the same
-- field-encryption pattern as users.totp_secret.
--
-- reminder_lead_days is SHARED: the per-owner lead window for BOTH the in-app
-- dashboard banner (issue #123) and these webhook reminders. Default 3 matches
-- the banner fallback constant services.DashboardReminderBannerWindowDays.
--
-- The two last_sent_cycle_start columns are watermarks storing the cycle-start
-- anchor date a period / ovulation reminder was last sent for, so the future
-- notify pass sends at most one reminder of each kind per cycle.
--
-- The migration runner skips any ADD COLUMN whose column already exists, so
-- this file is idempotent across clean installs and rolling deploys. Rollback
-- (forward-only repo) is documented in the commit body, not here.

ALTER TABLE users ADD COLUMN webhook_enabled BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN webhook_url TEXT;
ALTER TABLE users ADD COLUMN webhook_notify_period BOOLEAN NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN webhook_notify_ovulation BOOLEAN NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN webhook_period_last_sent_cycle_start DATE;
ALTER TABLE users ADD COLUMN webhook_ovulation_last_sent_cycle_start DATE;
ALTER TABLE users ADD COLUMN reminder_lead_days INTEGER NOT NULL DEFAULT 3;
