-- WEB-383 (GitHub #1071): owner-defined webhook reminders on a fixed cycle day
-- (for example a medication taken on cycle days 15 and 25).
--
-- cycle_day_reminders holds one row per rule. cycle_day counts from the last
-- cycle start the boundary function resolves (day 1 = the start itself).
-- remind_at_hour is the owner-local hour (0-23) from which the rule may be sent
-- on its day. label is owner-entered text shown on the settings page. It leaves
-- the server only when send_label is TRUE, an opt-in made per rule.
-- last_sent_on is the owner-local day the rule was last claimed for delivery,
-- the watermark that keeps a rule to one send per cycle.
--
-- users.webhook_cycle_day_hold_sent_start is the cycle start a single neutral
-- "cycle-day reminders paused" notice was last claimed for, so a cycle running
-- past its usual length produces that notice once and not once per rule.
--
-- The CHECK constraints are table-level so that no column definition carries
-- anything after its DEFAULT (the schema parity test reads a column's DEFAULT
-- up to the end of its definition).
--
-- Both are additive. Rollback: a binary that predates this migration ignores
-- the table and the column, so no reminder of this kind is sent. Dropping them
-- by hand is safe once no newer binary runs against the database.
--
-- NOTE: keep prose in this file free of semicolons -- the migration runner
-- splits statements on the semicolon character without stripping SQL comments,
-- so a semicolon inside a comment is mis-parsed as a statement boundary.

CREATE TABLE IF NOT EXISTS cycle_day_reminders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  cycle_day INTEGER NOT NULL,
  remind_at_hour INTEGER NOT NULL DEFAULT 9,
  label TEXT NOT NULL DEFAULT '',
  send_label BOOLEAN NOT NULL DEFAULT 0,
  last_sent_on DATE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CHECK (cycle_day BETWEEN 1 AND 90),
  CHECK (remind_at_hour BETWEEN 0 AND 23)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cycle_day_reminders_user_day_hour
  ON cycle_day_reminders (user_id, cycle_day, remind_at_hour);

ALTER TABLE users ADD COLUMN webhook_cycle_day_hold_sent_start DATE;
