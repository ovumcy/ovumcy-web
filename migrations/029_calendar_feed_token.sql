-- Calendar (.ics) feed subscription token (slice 1: token storage only).
--
-- Adds the two per-owner columns that back a pull-based calendar-feed
-- subscription: an owner can generate an opaque feed URL whose path carries a
-- bearer capability token, and a calendar client polls it for an .ics of the
-- owner's own predicted/recorded cycle events. Storage ONLY in this slice: no
-- endpoint, no .ics builder, no rate-limit/404-no-oracle wiring, no settings
-- UI (later slices).
--
-- The feed token is split SELECTOR + VERIFIER so a feed request (which carries
-- no email to look up by) resolves the row with a single indexed lookup instead
-- of an O(N) bcrypt scan over every user:
--
--   calendar_feed_selector       -- the NON-secret lookup id (high-entropy, but
--                                  it only NAMES the row). Stored in plaintext by
--                                  design as it is not a credential on its own,
--                                  and indexed for the by-selector lookup.
--   calendar_feed_verifier_hash  -- the bcrypt hash of the SECRET verifier half.
--                                  The verifier plaintext is NEVER stored. The
--                                  full token (selector+verifier) is shown to
--                                  the owner exactly once at generation and is
--                                  not retrievable afterward, mirroring the
--                                  recovery-code shown-once model. A feed request
--                                  supplies both halves in the URL path, and the
--                                  server looks up by selector then verifies the
--                                  verifier with a constant-time bcrypt compare.
--                                  A missing selector and a wrong verifier both
--                                  resolve to the same "not found" (no oracle).
--
-- Feed OFF by default: the model persists an empty string for a fresh/off feed
-- (a Go string zero value), so the UNIQUE index is PARTIAL -- it covers only
-- rows whose selector is non-empty. Without the predicate every feed-off row
-- would share the empty-string key and collide on the second insert. A selector
-- generated for an armed feed is guaranteed unique across owners so the
-- by-selector lookup resolves exactly one row.
--
-- Governance note: a bearer capability token in a URL path is a deliberate,
-- owner-approved carve-out to the "no secret in transport" invariant for the
-- feed surface, compensated by hashing-at-rest (this column), one-click revoke,
-- 404-no-oracle, per-IP rate-limit, and log-redaction (the compensating
-- controls land in later slices). The governance/docs edits land in a later
-- slice once the enforcing tests exist.
--
-- The migration runner skips any ADD COLUMN whose column already exists, so this
-- file is idempotent across clean installs and rolling deploys. Rollback
-- (forward-only repo) is documented in the commit body, not here.
--
-- NOTE: keep prose in this file free of semicolons -- the migration runner
-- splits statements on the semicolon character without stripping SQL comments,
-- so a semicolon inside a comment is mis-parsed as a statement boundary.

ALTER TABLE users ADD COLUMN calendar_feed_selector TEXT;
ALTER TABLE users ADD COLUMN calendar_feed_verifier_hash TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_calendar_feed_selector
    ON users (calendar_feed_selector)
    WHERE calendar_feed_selector <> '';
