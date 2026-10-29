-- WEB-282: which period days the auto-fill wrote. See
-- migrations/042_daily_logs_auto_filled.sql for the value contract: NULL for
-- rows written before this migration (the un-tick walk falls back to its flow
-- heuristic), TRUE from the auto-fills, FALSE from owner writes and import.
--
-- NOTE: keep prose in this file free of semicolons -- the migration runner
-- splits statements on the semicolon character without stripping SQL comments.

ALTER TABLE daily_logs ADD COLUMN IF NOT EXISTS auto_filled BOOLEAN;
