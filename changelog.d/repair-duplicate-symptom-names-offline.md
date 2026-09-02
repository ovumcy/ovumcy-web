### Fixed

- **An upgrade that a duplicate symptom name stops can now be finished.** Adding the per-account
  unique index on symptom names refuses when the database already holds two symptoms one account
  had under the same name, and that refusal is correct — it deletes nothing and names every
  conflicting group. What it did not have was a way out: the refusal is also what stops the
  server, so the instruction to resolve the groups "through the application" pointed at an
  application the refusal had already stopped, and every other operator subcommand applies
  migrations on the way in and met the same refusal.

  `ovumcy repair symptom-names` is that way out. It opens the database **without applying
  migrations**, which is what lets it reach the state that is stuck at all. Run on its own it
  inspects: it prints which symptom each account keeps and which rows would fold into it, changes
  nothing, and exits non-zero while duplicates remain, so an upgrade script can gate on it. Run
  with `--apply` it merges each group — every day log that named an absorbed symptom is
  re-pointed at the kept one **first, in the transaction that removes the rows**, so no day loses
  what the owner recorded, and a day that named both ends up naming one. A second run finds
  nothing and says so, which is also what a failed run leaves behind: the merge is one
  transaction, so a failure changes nothing and can simply be repeated.

  Which row is kept follows what the owner can still reach: an active row before an archived one,
  a built-in before a custom one, and the oldest of what remains. The built-in rule is not a
  preference — the reachable half of this class is the built-in seeding race, and a built-in
  symptom can be neither renamed, hidden nor removed from Settings, so leaving a second built-in
  behind would leave a duplicate in the day-entry picker with no surface anywhere that can clear
  it.

  The repair reads its groups through the database's own `lower(name)`, the same expression the
  index is built on, rather than folding names in Go. That is what makes one command correct on
  both engines where they disagree: PostgreSQL folds by locale and SQLite folds ASCII only, so a
  case-only variant of a non-ASCII name is a conflict on one and not the other — and the repair
  reports exactly what that engine's index would refuse, without needing to know which engine it
  is on. Both engines are covered by a test that takes a database holding duplicates through the
  documented steps to a completed start.

  Being the only entry point that opens a database of unknown schema version, it is also the only
  one that can be pointed at the wrong database and not notice — a SQLite path that does not exist
  is created empty rather than refused. So it checks for the symptom catalogue before any query
  assumes it, and answers a mistyped `DB_PATH` by naming the path it actually reached, instead of
  with the engine's `no such table` about the very table the operator was told to repair. A
  PostgreSQL target is named by `DATABASE_URL` and never by its value, which carries credentials.

  The refusal message now names this route and the runbook instead of the application.

- **`docs/self-hosted.md` says what a rollback past the last six migrations costs.** Downgrade
  Caveats stopped at migration 022, so everything added since was undocumented in both
  directions. It now covers 032 through 038: which are plain additive changes an older binary
  ignores, which one deletes rows and why that is not a loss, and the two whose protection is
  simply off while you are downgraded — the "shown exactly once" marks on the recovery code and
  the calendar-feed URL, and the webhook configuration counter. Migration 037 is called out as
  the opposite shape: safe to roll back, and the one that can refuse to roll forward.

- **A new operator runbook covers a migration refused by existing data.** It gives the whole
  sequence — stop the service, back up, inspect, apply, start — including the detail that the
  command is reached with `docker compose run` rather than `docker exec`, because the container
  `docker exec` would attach to is not up.
