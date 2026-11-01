none

Test-only. The SQLite/Postgres schema parity test compared tables, column
names and type families only, so a migration giving a column a different
`DEFAULT`, a different `NOT NULL`, or a missing or different index on one
engine kept it green. It now also compares each shared column's normalized
default, nullability and primary-key membership, and every secondary index —
uniqueness, key list including expressions, and partial predicate — read from
each engine's catalog, since GORM's own index reader fails on SQLite
expression indexes and drops expressions on Postgres. The one known default
divergence, `users.auto_period_fill` from migration 035, is waived by name and
fails as stale once the trees agree. Load-bearing values are anchored by name:
`users.auth_session_version DEFAULT 1 NOT NULL`, the normalized-email unique
index, the calendar-feed partial index and `daily_logs`' one-row-per-day
constraint. No production code or migration changed, and no other drift was
found.
