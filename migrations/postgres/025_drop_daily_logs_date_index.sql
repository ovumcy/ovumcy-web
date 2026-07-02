-- 025 drops idx_daily_logs_date on Postgres for parity with the SQLite side.
-- The standalone index on daily_logs(date) has no consumer: every daily_logs
-- query is user-scoped and filters on user_id first, so the composite
-- UNIQUE(user_id, date) index already covers those (user_id, date) scans. The
-- bare-date index only adds write cost with no read benefit for the per-user
-- access pattern.
--
-- Rollback: CREATE INDEX IF NOT EXISTS idx_daily_logs_date ON daily_logs(date);

DROP INDEX IF EXISTS idx_daily_logs_date;
