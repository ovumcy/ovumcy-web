-- 025 drops idx_daily_logs_date, the standalone index on daily_logs(date).
-- It was created by migration 001 and recreated by the table rebuilds in
-- migrations 003 and 024, but it has no consumer: every daily_logs query in the
-- codebase is user-scoped and filters on user_id first (verified across
-- internal/db repositories), so the composite UNIQUE(user_id, date) index —
-- which leads with user_id and covers (user_id, date) range scans — already
-- serves those paths. A bare-date index only adds write-amplification and disk
-- with no read benefit for a single-tenant, per-user access pattern.
--
-- Rollback: CREATE INDEX IF NOT EXISTS idx_daily_logs_date ON daily_logs(date);

DROP INDEX IF EXISTS idx_daily_logs_date;
