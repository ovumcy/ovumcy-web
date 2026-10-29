-- WEB-282: which period days the auto-fill wrote. Nullable, no default, no
-- backfill. NULL is the honest value for every row written before this
-- migration -- provenance was never recorded -- and the un-tick walk falls
-- back to its flow heuristic for exactly those rows.
--
-- 1 is written only by the period auto-fills (the fill behind a logged period
-- start, its running-period catch-up and the onboarding fill). 0 is written by
-- every owner write of the day and by import, which carries no provenance and
-- so lands as the owner's own record. The un-tick walk clears a 1 day, keeps a
-- 0 day, and judges a NULL day by flow as before.
--
-- No index: read only on the one row a locking read already addressed.
--
-- NOTE: keep prose in this file free of semicolons -- the migration runner
-- splits statements on the semicolon character without stripping SQL comments.

ALTER TABLE daily_logs ADD COLUMN auto_filled BOOLEAN;
