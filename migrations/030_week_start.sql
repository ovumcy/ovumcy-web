-- Week-start display preference (issue #225).
--
-- Adds a per-owner preference for the first day of the week used by the
-- calendar grid and weekday header. Values are 'sunday' or 'monday'. The
-- default is 'sunday' so existing installs keep their current Sunday-first
-- layout -- Monday is opt-in from the settings tracking form.
--
-- Not sensitive, not a secret: a pure presentation preference, owner-scoped
-- like the other display toggles (temperature_unit, show_historical_phases).
--
-- The migration runner skips any ADD COLUMN whose column already exists, so this
-- file is idempotent across clean installs and rolling deploys. Rollback
-- (forward-only repo) is documented in the commit body, not here.

ALTER TABLE users ADD COLUMN week_starts_on TEXT NOT NULL DEFAULT 'sunday';
