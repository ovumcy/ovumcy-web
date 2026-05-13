-- Persist TOTP replay-protection state so the 90s reuse window survives a
-- process restart or rolling deploy (was previously held only in an in-memory
-- sync.Map).
--
-- We store the RFC 6238 time step (floor(unix_seconds / 30)) of the last
-- successfully consumed code. ClaimTOTPStep does an atomic
--   UPDATE users SET totp_last_used_step = ?
--   WHERE id = ? AND totp_last_used_step < ?
-- so a replayed code (same or older step) and concurrent submissions of the
-- same step collapse to a single winner.

ALTER TABLE users ADD COLUMN totp_last_used_step BIGINT NOT NULL DEFAULT 0;
