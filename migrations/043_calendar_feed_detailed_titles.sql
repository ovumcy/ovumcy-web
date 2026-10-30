-- WEB-379 (GitHub #1159): the owner's opt-in to name the estimate type in the
-- .ics feed's event titles. FALSE (the default, and every existing row) keeps
-- the single fixed neutral SUMMARY byte-for-byte. TRUE renders the localized
-- period / ovulation titles, each still carrying the estimate qualifier.
--
-- Written only by SaveCalendarFeedToken, from the choice made on the generate
-- or rotate form, in the same UPDATE as the token it describes. Revoke and the
-- bulk disarms leave it standing, exactly like calendar_feed_key_epoch: a flag
-- with no token publishes nothing, and the next mint always overwrites it.
--
-- NOT NULL DEFAULT 0: an upgraded row reads as neutral without a backfill.
-- A rollback to a binary that predates this column ignores it, so every feed
-- falls back to the neutral title. Rollback (forward-only repo) is documented
-- in the commit body, not here.
--
-- NOTE: keep prose in this file free of semicolons -- the migration runner
-- splits statements on the semicolon character without stripping SQL comments,
-- so a semicolon inside a comment is mis-parsed as a statement boundary.

ALTER TABLE users ADD COLUMN calendar_feed_detailed_titles BOOLEAN NOT NULL DEFAULT 0;
