### Fixed

- **A missing `schema_migrations` row no longer costs health data.** The migration runner
  re-applies any migration whose ledger row is absent — a restore from a backup taken before the
  row was written, or a pruned bookkeeping table — and what made that safe was a single skip that
  recognizes `ALTER TABLE ... ADD COLUMN` and nothing else. Migration 003 reconciles `daily_logs`
  by rebuilding it, and its replacement table is the table as it looked at version 003: replaying
  it on a database that later migrations had widened copied the nine columns of migration 001 and
  dropped `mood`, `sex_activity`, `bbt`, `cervical_mucus`, `cycle_start`, `is_uncertain`,
  `cycle_factor_keys` and `pregnancy_test` — eight health columns and every value in them — on an
  instance that was already fully migrated, with the boot reporting success. Migration 024 has the
  same shape and is destructive today only because nothing was added to `daily_logs` after it.

  The runner now refuses a migration that drops a table when the database has moved past it,
  measured two ways so neither state of the ledger is uncovered: a recorded LATER version means
  the migration is spent and nothing is executed at all, and — for a ledger lost entirely, where
  no later version exists to compare against — a table that comes back from a rebuild without a
  column it had before fails the migration inside its own transaction, so every statement it ran
  is rolled back. Either way the database is left exactly as it was, and the refusal names the
  migration, the table, the columns it protected and what to restore. A migration that means to
  remove a column still does it the visible way, with an explicit `DROP COLUMN`, which the check
  does not look at.

  Nothing changes for a clean install or a normal upgrade: on both, no migration numbered above
  the one being applied is recorded, and the rebuilds preserve every column they copy. What
  changes is that the one situation in which a boot used to destroy records now stops instead.

### Internal

- **The migration replay path is swept rather than sampled.** A new `internal/db` test deletes
  each `schema_migrations` row in turn from a fully migrated database, reopens it through the real
  boot path, and requires that the reopen cost neither a column nor a stored value — passing on
  either outcome, a completed boot or a refusal by name, since both are non-destructive. Two cases
  spell out the failures behind it: removing only the row for 003 on a current database, and
  losing the whole ledger, each with a seeded day carrying a value in every column added after
  003. It is deliberately narrower than "the schema is unchanged", and the file says so:
  indexes are out of scope, because migrations 001 and 024 both end with
  `CREATE INDEX IF NOT EXISTS idx_daily_logs_date`, which migration 025 later drops, so replaying
  either recreates a redundant index — write amplification, never a column or a row. Because the
  sweep runs on SQLite, where the rebuild pattern lives, a second test asserts the premise that
  makes that enough: the Postgres tree drops no table at all today, and a rebuild landing there
  later fails until the sweep is extended to it.

- **An account with a role this product does not have is refused where it is written.** Ovumcy is
  owner-role-only — every account is the sole owner of its own data, and there is no viewer or
  partner role — but the `users` column has carried `CHECK (role IN ('owner', 'partner'))` since
  the initial schema on both engines, while nothing else in the tree mentions `partner`: no
  constant, no handler, no template, no locale key. The database would have accepted such an
  account and only the login policy would have turned it away afterwards. Both account-creating
  repository methods now reject any role other than owner before the insert, so the refused
  account leaves no row and no seeded symptom behind; an unset role still means "the column
  default", which is written out as owner rather than left to the tag and the migration agreeing.
  The constraint itself is left alone on purpose and the reason is recorded next to the check:
  SQLite has no `ALTER` for a `CHECK`, so narrowing it means rebuilding `users` — and with
  `foreign_keys=ON` a dropped table fires every `ON DELETE CASCADE` hanging off it, which would
  make a migration that tightened one unused constant delete every day log on the instance.
