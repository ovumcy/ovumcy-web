-- 024 makes daily_logs.bbt honest on Postgres. The column was created
-- (migration 006) as `DOUBLE PRECISION NOT NULL DEFAULT 0`, where 0 was an
-- in-band sentinel meaning "not measured". Postgres supports in-place column
-- alteration, so unlike the SQLite side there is no table rebuild: drop the
-- NOT NULL and DEFAULT, then rewrite the sentinel to NULL.
--
-- Idempotent: DROP NOT NULL / DROP DEFAULT are no-ops once already applied, and
-- the UPDATE rewrites only rows still carrying the exact sentinel 0.
--
-- Rollback: UPDATE daily_logs SET bbt = 0 WHERE bbt IS NULL;
--           ALTER TABLE daily_logs ALTER COLUMN bbt SET DEFAULT 0;
--           ALTER TABLE daily_logs ALTER COLUMN bbt SET NOT NULL;

ALTER TABLE daily_logs ALTER COLUMN bbt DROP NOT NULL;
ALTER TABLE daily_logs ALTER COLUMN bbt DROP DEFAULT;

UPDATE daily_logs SET bbt = NULL WHERE bbt = 0;
