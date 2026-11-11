-- WEB-381 (GitHub #1160): two more per-link opt-ins of the .ics feed, each
-- chosen on the generate or rotate form like calendar_feed_detailed_titles
-- (migration 043). calendar_feed_period_span sends each predicted period as a
-- multi-day span instead of its start day alone. calendar_feed_fertile_window
-- adds the estimated fertile window the calendar grid shades. FALSE (the
-- default, and every existing row) keeps the feed body exactly as before.
--
-- Written only by SaveCalendarFeedToken, in the same UPDATE as the token it
-- describes, and reset to FALSE by every revoke, disarm and clear-data write
-- together with the token columns. The same holds for
-- calendar_feed_detailed_titles since WEB-389, which corrects the prose of
-- migrations/043_calendar_feed_detailed_titles.sql that says the disarms leave
-- that flag standing.
--
-- NOT NULL DEFAULT 0: an upgraded row reads as opted out without a backfill.
-- A rollback to a binary that predates these columns ignores them, so every
-- feed falls back to single estimated days. Rollback (forward-only repo) is
-- documented in the commit body, not here.
--
-- NOTE: keep prose in this file free of semicolons -- the migration runner
-- splits statements on the semicolon character without stripping SQL comments,
-- so a semicolon inside a comment is mis-parsed as a statement boundary.

ALTER TABLE users ADD COLUMN calendar_feed_period_span BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN calendar_feed_fertile_window BOOLEAN NOT NULL DEFAULT 0;
