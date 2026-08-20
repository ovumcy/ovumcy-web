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

  The runner now refuses a migration that narrows the schema it is applied to, measured two ways
  so neither state of the ledger is uncovered. A recorded LATER version means a migration that
  drops a table is spent, so nothing is executed at all. And — for a ledger lost entirely, where
  no later version exists to compare against — the columns of every table are read before the
  migration's first statement and again after its last, inside the migration's own transaction,
  so a table that ends up without a column it had before fails the migration and every statement
  it ran is rolled back. Reading the whole schema rather than the tables a migration names is
  what makes that check about the effect instead of the spelling: SQLite rebuilds a table either
  by dropping the original and renaming a replacement onto its name, as migrations 003 and 024
  do, or by renaming the original away first and recreating it — the second narrows the table
  just as much while dropping only a name that did not exist when the migration began. Either
  way the database is left exactly as it was, and the refusal names the migration, the table, the
  columns it protected and what to restore.

  Both losses stay expressible, each by something the author writes out and names: a column
  removed by an explicit `ALTER TABLE … DROP COLUMN` in the migration's own SQL, and a table
  retired for good by the marker line `-- ovumcy:removes-table <name>`, which authorizes the one
  table it names. A bare `DROP TABLE` is deliberately not read as consent — it is the middle
  statement of every rebuild in the tree, and reading it as consent is how migration 003 came to
  discard eight health columns while reporting success.

  Nothing changes for a clean install or a normal upgrade beyond the time it takes: on both, no
  migration numbered above the one being applied is recorded, and the rebuilds preserve every
  column they copy. Reading the schema between statements costs about 150 ms once, over a clean
  SQLite bootstrap of the whole set (mean 300 ms against 113 ms over twenty runs); an instance
  with no migration to apply pays nothing, because the ledger check comes first. What changes is
  that the one situation in which a boot used to destroy records now stops instead.

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

  The shapes the embedded tree does not contain are covered by synthetic migrations run through
  the same code path: both SQLite rebuild idioms, each in a narrowing and a preserving variant so
  the refusal cannot be credited to a check that refuses everything; a table removal with the
  marker, without it, with a marker naming a different table, and with the marker words buried in
  prose; and an explicit column drop, which must still apply.

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
