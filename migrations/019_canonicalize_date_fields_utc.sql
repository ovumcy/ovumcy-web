-- Canonicalize date-only stored values to UTC-midnight to match the
-- on-disk shape produced by the DailyLog BeforeSave hook.
--
-- glebarez/sqlite stores DATE columns as TEXT. Legacy rows from before
-- the BeforeSave hook may carry a non-UTC offset that reflects the
-- request locale at write time, or be bare YYYY-MM-DD strings inserted
-- via raw SQL. Read-side services.CalendarDay already handles mixed
-- shapes correctly, so the application is unaffected by mixed storage.
-- This migration aligns the on-disk shape so future range queries do
-- not need to defend against legacy variants.
--
-- The rewrite extracts the YYYY-MM-DD prefix that all glebarez DATE
-- serializations share and rebuilds it at UTC-midnight. The user
-- intended calendar day is preserved across all source offsets because
-- a row stored as `2026-02-10 00:00:00-05:00` represents calendar day
-- 2026-02-10 in the writing locale, and the prefix slice returns the
-- same `2026-02-10` regardless of the offset suffix.
--
-- Idempotent: applied to an already-canonical value, the prefix slice
-- returns the same calendar day and the suffix is the same canonical
-- string.

UPDATE daily_logs
SET date = substr(date, 1, 10) || ' 00:00:00+00:00'
WHERE date IS NOT NULL;

UPDATE users
SET last_period_start = substr(last_period_start, 1, 10) || ' 00:00:00+00:00'
WHERE last_period_start IS NOT NULL;
