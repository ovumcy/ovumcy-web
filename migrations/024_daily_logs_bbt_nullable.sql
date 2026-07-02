-- 024 makes daily_logs.bbt honest: the column was created (migration 006) as
-- `REAL NOT NULL DEFAULT 0`, where the value 0 was an in-band sentinel meaning
-- "not measured". 0 degrees C is not a physiological basal body temperature, so
-- the sentinel and a real reading were distinguishable only by convention. This
-- migration drops NOT NULL / DEFAULT 0 and rewrites every sentinel 0 to NULL so
-- "not measured" is represented honestly as NULL.
--
-- SQLite cannot ALTER COLUMN ... DROP NOT NULL / DROP DEFAULT in place, so we
-- follow the established table-rebuild reconcile pattern (migration 003): create
-- a replacement table with the target schema, copy all rows (mapping bbt = 0 to
-- NULL in the same pass), swap, and restore the supporting indexes. The rebuild
-- reproduces the current daily_logs shape exactly except for the bbt column.
--
-- Rollback: rebuild daily_logs with `bbt REAL NOT NULL DEFAULT 0` and
-- `UPDATE daily_logs SET bbt = 0 WHERE bbt IS NULL` before copying back.

-- 1) Replacement table: identical to the live schema, but bbt is nullable with
-- no default. Column order and every other definition are preserved so the
-- rebuild is a pure nullability change.
CREATE TABLE daily_logs_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  date DATE NOT NULL,
  is_period BOOLEAN NOT NULL DEFAULT 0,
  flow TEXT NOT NULL DEFAULT 'none',
  symptom_ids TEXT,
  notes TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  mood INTEGER NOT NULL DEFAULT 0,
  sex_activity TEXT NOT NULL DEFAULT 'none',
  bbt REAL,
  cervical_mucus TEXT NOT NULL DEFAULT 'none',
  cycle_start BOOLEAN NOT NULL DEFAULT 0,
  is_uncertain BOOLEAN NOT NULL DEFAULT 0,
  cycle_factor_keys TEXT NOT NULL DEFAULT '[]',
  pregnancy_test TEXT NOT NULL DEFAULT 'none',
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  UNIQUE(user_id, date)
);

-- 2) Copy every row, converting the legacy sentinel to NULL. A value of exactly
-- 0 (the old "unset" marker) becomes NULL, while a genuine reading is copied
-- verbatim. Idempotent on re-run because a NULL stays NULL and a reading stays.
INSERT INTO daily_logs_new (
  id, user_id, date, is_period, flow, symptom_ids, notes, created_at, updated_at,
  mood, sex_activity, bbt, cervical_mucus, cycle_start, is_uncertain,
  cycle_factor_keys, pregnancy_test
)
SELECT
  id, user_id, date, is_period, flow, symptom_ids, notes, created_at, updated_at,
  mood, sex_activity,
  CASE WHEN bbt = 0 THEN NULL ELSE bbt END,
  cervical_mucus, cycle_start, is_uncertain,
  cycle_factor_keys, pregnancy_test
FROM daily_logs;

-- 3) Swap old and new tables only after a successful copy.
DROP TABLE daily_logs;
ALTER TABLE daily_logs_new RENAME TO daily_logs;

-- 4) Restore supporting indexes expected by query paths. idx_daily_logs_date is
-- recreated here for parity with migration 003. Migration 025 then removes it as
-- redundant with the user-scoped UNIQUE(user_id, date) index.
CREATE INDEX IF NOT EXISTS idx_daily_logs_user_id ON daily_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_daily_logs_date ON daily_logs(date);

-- 5) Keep AUTOINCREMENT sequence aligned with migrated IDs.
INSERT OR REPLACE INTO sqlite_sequence(name, seq)
SELECT 'daily_logs', COALESCE(MAX(id), 0) FROM daily_logs;
